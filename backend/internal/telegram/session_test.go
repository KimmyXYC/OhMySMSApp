package telegram

import (
	"sync"
	"testing"
	"time"
)

func TestSessionStore_BasicCRUD(t *testing.T) {
	s := newSessionStore(5 * time.Minute)
	if s.Get(1) != nil {
		t.Fatal("expected nil for empty")
	}
	s.Set(1, &Session{Kind: SessionSendSMS, Step: StepAwaitModem})
	got := s.Get(1)
	if got == nil || got.Kind != SessionSendSMS {
		t.Fatalf("get wrong: %+v", got)
	}
	ok := s.Update(1, func(x *Session) { x.Peer = "+123"; x.Step = StepAwaitText })
	if !ok {
		t.Fatal("update should succeed")
	}
	got = s.Get(1)
	if got.Peer != "+123" || got.Step != StepAwaitText {
		t.Fatalf("update mismatch: %+v", got)
	}
	s.Delete(1)
	if s.Get(1) != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestSessionStore_TTLExpiry(t *testing.T) {
	s := newSessionStore(50 * time.Millisecond)
	s.Set(1, &Session{Kind: SessionUSSD, Step: StepAwaitModem})
	if s.Get(1) == nil {
		t.Fatal("should exist")
	}
	time.Sleep(80 * time.Millisecond)
	if s.Get(1) != nil {
		t.Fatal("should expire")
	}
	// Update on expired should return false
	s.Set(2, &Session{Kind: SessionSendSMS})
	time.Sleep(80 * time.Millisecond)
	if s.Update(2, func(x *Session) {}) {
		t.Fatal("expired update should be false")
	}
}

func TestSessionStore_Purge(t *testing.T) {
	s := newSessionStore(30 * time.Millisecond)
	s.Set(1, &Session{Kind: SessionSendSMS})
	s.Set(2, &Session{Kind: SessionSendSMS})
	time.Sleep(60 * time.Millisecond)
	s.Set(3, &Session{Kind: SessionSendSMS}) // 新鲜
	s.Purge()
	if s.Get(1) != nil || s.Get(2) != nil {
		t.Fatal("expected 1,2 purged")
	}
	if s.Get(3) == nil {
		t.Fatal("3 should survive")
	}
}

func TestSessionStore_Concurrent(t *testing.T) {
	s := newSessionStore(1 * time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			s.Set(id, &Session{Kind: SessionSendSMS})
			s.Update(id, func(x *Session) { x.Peer = "x" })
			_ = s.Get(id)
		}(int64(i))
	}
	wg.Wait()
	for i := int64(0); i < 20; i++ {
		if s.Get(i) == nil {
			t.Fatalf("session %d missing", i)
		}
	}
}
