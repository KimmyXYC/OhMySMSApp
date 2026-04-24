package modem

import (
	"context"
	"testing"
	"time"
)

// TestMockProviderEvents 验证 MockProvider 启动后能 emit ModemAdded 与 SignalSampled。
func TestMockProviderEvents(t *testing.T) {
	p := NewMockProvider(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	go func() { _ = p.Start(ctx) }()

	events := p.Events()
	addedDevices := map[string]bool{}
	signalDevices := map[string]bool{}

	deadline := time.After(6 * time.Second)
	for {
		select {
		case ev := <-events:
			switch ev.Kind {
			case EventModemAdded:
				addedDevices[ev.DeviceID] = true
			case EventSignalSampled:
				signalDevices[ev.DeviceID] = true
			}
			if len(addedDevices) >= 2 && len(signalDevices) >= 2 {
				return // pass
			}
		case <-deadline:
			t.Fatalf("timeout: added=%d signal=%d", len(addedDevices), len(signalDevices))
		}
	}
}

// TestMockProviderSendSMS 验证 mock send 后事件与 ListSMS。
func TestMockProviderSendSMS(t *testing.T) {
	p := NewMockProvider(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = p.Start(ctx) }()

	// 等 ModemAdded 事件
	var deviceID string
	events := p.Events()
waitAdded:
	for {
		select {
		case ev := <-events:
			if ev.Kind == EventModemAdded {
				deviceID = ev.DeviceID
				break waitAdded
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no ModemAdded in time")
		}
	}

	extID, err := p.SendSMS(ctx, deviceID, "+1234567890", "hello")
	if err != nil {
		t.Fatalf("send sms: %v", err)
	}
	if extID == "" {
		t.Fatal("empty extID")
	}

	list, err := p.ListSMS(deviceID)
	if err != nil {
		t.Fatalf("list sms: %v", err)
	}
	if len(list) != 1 || list[0].Peer != "+1234567890" {
		t.Fatalf("unexpected sms list: %+v", list)
	}
}

// TestMockProviderUSSD 验证 USSD mock 返回值。
func TestMockProviderUSSD(t *testing.T) {
	p := NewMockProvider(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = p.Start(ctx) }()

	// 选一台支持 USSD 的 modem（第一台 pSIM）
	var deviceID string
	for _, m := range p.ListModems() {
		if m.HasUSSD {
			deviceID = m.DeviceID
			break
		}
	}
	if deviceID == "" {
		t.Fatal("no ussd-capable mock modem")
	}
	sid, reply, err := p.InitiateUSSD(ctx, deviceID, "*101#")
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if sid == "" || reply == "" {
		t.Fatalf("empty sid/reply: %q %q", sid, reply)
	}
	if !contains(reply, "balance") {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
