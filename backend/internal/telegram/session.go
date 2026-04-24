package telegram

import (
	"sync"
	"time"
)

// SessionKind 标识多轮交互的类型。
type SessionKind string

const (
	SessionSendSMS SessionKind = "send_sms"
	SessionUSSD    SessionKind = "ussd"
	SessionReply   SessionKind = "reply" // 对已有短信回复（已预填 modem+peer）
)

// SessionStep 状态机当前步骤。
type SessionStep string

const (
	StepAwaitModem SessionStep = "await_modem"
	StepAwaitPeer  SessionStep = "await_peer"
	StepAwaitText  SessionStep = "await_text"
	StepConfirm    SessionStep = "confirm"
	StepUSSDAwaitResponse SessionStep = "ussd_await_response" // USSD 进入 user_response 态，等 bot 侧输入
	StepDone       SessionStep = "done"
	StepCancelled  SessionStep = "cancelled"
)

// Session 是一次多轮交互的状态。
type Session struct {
	Kind     SessionKind
	Step     SessionStep
	DeviceID string // 选中的 modem
	Peer     string
	Text     string

	// USSD 特有
	USSDSessionID string // provider 返回的 session id

	// 为了继续性回复用：上一次 bot 发出的 prompt 的 message id（用于可选的 edit）
	PromptMsgID int

	CreatedAt time.Time
	UpdatedAt time.Time
}

// sessionStore 按 chatID 索引 Session。
type sessionStore struct {
	mu   sync.Mutex
	data map[int64]*Session
	ttl  time.Duration
}

func newSessionStore(ttl time.Duration) *sessionStore {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &sessionStore{
		data: make(map[int64]*Session),
		ttl:  ttl,
	}
}

// Get 返回当前 session 并处理 TTL 过期（过期则删除并返回 nil）。
func (s *sessionStore) Get(chatID int64) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.data[chatID]
	if !ok {
		return nil
	}
	if time.Since(sess.UpdatedAt) > s.ttl {
		delete(s.data, chatID)
		return nil
	}
	return sess
}

// Set 覆盖写入。
func (s *sessionStore) Set(chatID int64, sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	sess.UpdatedAt = now
	s.data[chatID] = sess
}

// Update 对已有 session 原地修改（也会刷新 UpdatedAt）。没有则 no-op 并返回 false。
func (s *sessionStore) Update(chatID int64, fn func(*Session)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.data[chatID]
	if !ok {
		return false
	}
	if time.Since(sess.UpdatedAt) > s.ttl {
		delete(s.data, chatID)
		return false
	}
	fn(sess)
	sess.UpdatedAt = time.Now()
	return true
}

// Delete 删除 session。
func (s *sessionStore) Delete(chatID int64) {
	s.mu.Lock()
	delete(s.data, chatID)
	s.mu.Unlock()
}

// Purge 清理过期的（供后台 ticker 调用）。
func (s *sessionStore) Purge() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.data {
		if now.Sub(v.UpdatedAt) > s.ttl {
			delete(s.data, k)
		}
	}
}
