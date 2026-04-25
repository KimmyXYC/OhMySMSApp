package telegram

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// fakeBotAPI 记录所有 Send 调用，供断言使用。
type fakeBotAPI struct {
	mu       sync.Mutex
	sent     []tgbotapi.Chattable
	requests []tgbotapi.Chattable
	updates  chan tgbotapi.Update
}

func newFakeBotAPI() *fakeBotAPI {
	return &fakeBotAPI{updates: make(chan tgbotapi.Update, 4)}
}

func (f *fakeBotAPI) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, c)
	return tgbotapi.Message{MessageID: len(f.sent)}, nil
}

func (f *fakeBotAPI) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, c)
	return &tgbotapi.APIResponse{Ok: true}, nil
}

func (f *fakeBotAPI) GetUpdatesChan(_ tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	return f.updates
}

func (f *fakeBotAPI) StopReceivingUpdates() {
	// idempotent close
	defer func() { _ = recover() }()
	close(f.updates)
}

func (f *fakeBotAPI) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeBotAPI) lastMessageText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return ""
	}
	if m, ok := f.sent[len(f.sent)-1].(tgbotapi.MessageConfig); ok {
		return m.Text
	}
	return ""
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testBot 构造一个带 fakeBotAPI 的 bot（不调用 run，避免后台 goroutine 干扰）。
func testBot(t *testing.T, provider modem.Provider) (*bot, *fakeBotAPI) {
	t.Helper()
	fb := newFakeBotAPI()
	b := newBotWithAPI(context.Background(), fb, 12345, true,
		provider, nil, nil, nil, discardLogger())
	return b, fb
}

func TestPushSMSReceived(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)

	// 找一个真实存在的 deviceID
	modems := prov.ListModems()
	if len(modems) == 0 {
		t.Fatal("mock provider should have modems")
	}
	dev := modems[0].DeviceID

	ev := modem.Event{
		Kind:     modem.EventSMSReceived,
		DeviceID: dev,
		Payload: modem.SMSRecord{
			Peer: "+1234567890",
			Text: "code: 654321",
		},
	}
	b.dispatchEvent(ev)

	if fb.sentCount() != 1 {
		t.Fatalf("expected 1 send, got %d", fb.sentCount())
	}
	txt := fb.lastMessageText()
	if !strings.Contains(txt, "新短信") || !strings.Contains(txt, "654321") {
		t.Errorf("unexpected push text: %s", txt)
	}
	// 应有 inline keyboard
	mc, ok := fb.sent[0].(tgbotapi.MessageConfig)
	if !ok {
		t.Fatalf("expected MessageConfig, got %T", fb.sent[0])
	}
	if mc.ReplyMarkup == nil {
		t.Error("expected reply markup (↩️ 回复 button)")
	}
}

func TestPushSMS_DisabledWhenPushSMSFalse(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	fb := newFakeBotAPI()
	b := newBotWithAPI(context.Background(), fb, 12345, false,
		prov, nil, nil, nil, discardLogger())

	dev := prov.ListModems()[0].DeviceID
	b.dispatchEvent(modem.Event{
		Kind:     modem.EventSMSReceived,
		DeviceID: dev,
		Payload:  modem.SMSRecord{Peer: "+1", Text: "x"},
	})
	if fb.sentCount() != 0 {
		t.Errorf("expected no sends with pushSMS=false, got %d", fb.sentCount())
	}
}

func TestPushModemOnline(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)
	b.dispatchEvent(modem.Event{
		Kind:     modem.EventModemAdded,
		DeviceID: "dev-x",
		Payload: modem.ModemState{
			DeviceID: "dev-x", Model: "EC20F",
			IMEI: "861234567899306",
			SIM: &modem.SimState{
				ICCID:        "8949000001234852F",
				MSISDN:       "+491791566795",
				OperatorName: "CHINA MOBILE",
			},
			SignalRecent:  true,
			SignalQuality: 70,
			AccessTech:    []string{"lte"},
		},
	})
	if fb.sentCount() != 1 {
		t.Fatalf("expected 1 send, got %d", fb.sentCount())
	}
	txt := fb.lastMessageText()
	for _, want := range []string{"上线", "EC20F", "9306", "491791566795", "CHINA MOBILE", "70"} {
		if !strings.Contains(txt, want) {
			t.Errorf("online text missing %q:\n%s", want, txt)
		}
	}
}

func TestPushModemOffline(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)
	b.dispatchEvent(modem.Event{
		Kind:     modem.EventModemRemoved,
		DeviceID: "dev-x",
		Payload: modem.ModemState{
			DeviceID: "dev-x", Model: "EC20F",
			IMEI: "861234567899306",
		},
	})
	if fb.sentCount() != 1 {
		t.Fatal("expected one send")
	}
	txt := fb.lastMessageText()
	if !strings.Contains(txt, "离线") {
		t.Errorf("unexpected: %s", txt)
	}
	if !strings.Contains(txt, "9306") {
		t.Errorf("offline text should contain IMEI tail: %s", txt)
	}
}

// TestPushModemAdded_SeedsSIMSnapshot 验证 ModemAdded 时只推一条上线消息，
// 不会额外推 SIM 插入消息（SIM 信息已包含在上线消息里）。
func TestPushModemAdded_SeedsSIMSnapshot(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)
	st := modem.ModemState{
		DeviceID: "dev-y", Model: "EC20F",
		SIM: &modem.SimState{ICCID: "111", OperatorName: "OP"},
	}
	b.dispatchEvent(modem.Event{Kind: modem.EventModemAdded, DeviceID: "dev-y", Payload: st})
	if fb.sentCount() != 1 {
		t.Fatalf("expected 1 send (online only), got %d", fb.sentCount())
	}
	// 紧接着同样的 ModemUpdated 不应触发 SIM 插入推送。
	b.dispatchEvent(modem.Event{Kind: modem.EventModemUpdated, DeviceID: "dev-y", Payload: st})
	if fb.sentCount() != 1 {
		t.Fatalf("ModemUpdated with same SIM should not push, got %d", fb.sentCount())
	}
}

// TestPushSIMInserted: ModemAdded 时无 SIM；后续 ModemUpdated 出现 SIM → 推送 SIM 插入。
func TestPushSIMInserted(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)
	dev := "dev-ins"
	b.dispatchEvent(modem.Event{
		Kind: modem.EventModemAdded, DeviceID: dev,
		Payload: modem.ModemState{DeviceID: dev, Model: "EC20F"}, // no SIM
	})
	// 第一条是上线消息
	if fb.sentCount() != 1 {
		t.Fatalf("expected 1 online send, got %d", fb.sentCount())
	}

	b.dispatchEvent(modem.Event{
		Kind: modem.EventModemUpdated, DeviceID: dev,
		Payload: modem.ModemState{
			DeviceID: dev, Model: "EC20F",
			SIM: &modem.SimState{
				ICCID: "8949000001234852F", MSISDN: "+491791566795", OperatorName: "CHINA MOBILE",
			},
		},
	})
	if fb.sentCount() != 2 {
		t.Fatalf("expected SIM insert push, total=%d", fb.sentCount())
	}
	txt := fb.lastMessageText()
	for _, want := range []string{"SIM 插入", "EC20F", "4852F", "491791566795", "CHINA MOBILE"} {
		if !strings.Contains(txt, want) {
			t.Errorf("insert text missing %q:\n%s", want, txt)
		}
	}
}

// TestPushSIMRemoved: 之前有 SIM；ModemUpdated 时 SIM=nil → 推送 SIM 拔出，并显示原 SIM 信息。
func TestPushSIMRemoved(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)
	dev := "dev-rem"
	b.dispatchEvent(modem.Event{
		Kind: modem.EventModemAdded, DeviceID: dev,
		Payload: modem.ModemState{
			DeviceID: dev, Model: "EC20F",
			SIM: &modem.SimState{ICCID: "8949000001234852F", OperatorName: "CHINA MOBILE"},
		},
	})
	if fb.sentCount() != 1 {
		t.Fatalf("expected 1 online send, got %d", fb.sentCount())
	}

	b.dispatchEvent(modem.Event{
		Kind: modem.EventModemUpdated, DeviceID: dev,
		Payload: modem.ModemState{DeviceID: dev, Model: "EC20F"}, // SIM=nil
	})
	if fb.sentCount() != 2 {
		t.Fatalf("expected SIM removed push, total=%d", fb.sentCount())
	}
	txt := fb.lastMessageText()
	for _, want := range []string{"SIM 拔出", "EC20F", "4852F", "CHINA MOBILE"} {
		if !strings.Contains(txt, want) {
			t.Errorf("remove text missing %q:\n%s", want, txt)
		}
	}
}

// TestPushSIMReplaced: SIM ICCID 变化 → 推送替换。
func TestPushSIMReplaced(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)
	dev := "dev-rep"
	b.dispatchEvent(modem.Event{
		Kind: modem.EventModemAdded, DeviceID: dev,
		Payload: modem.ModemState{
			DeviceID: dev, Model: "EC20F",
			SIM: &modem.SimState{ICCID: "8949000001234852F", OperatorName: "OLD"},
		},
	})

	b.dispatchEvent(modem.Event{
		Kind: modem.EventModemUpdated, DeviceID: dev,
		Payload: modem.ModemState{
			DeviceID: dev, Model: "EC20F",
			SIM: &modem.SimState{ICCID: "8949000009999301A", OperatorName: "NEW"},
		},
	})
	if fb.sentCount() != 2 {
		t.Fatalf("expected SIM replaced push, total=%d", fb.sentCount())
	}
	txt := fb.lastMessageText()
	for _, want := range []string{"SIM 替换", "4852F", "9301A", "NEW"} {
		if !strings.Contains(txt, want) {
			t.Errorf("replace text missing %q:\n%s", want, txt)
		}
	}
}

// TestPushSIM_NoChangeNoSpam: 多次 ModemUpdated 携带相同 SIM 不应触发任何推送。
func TestPushSIM_NoChangeNoSpam(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)
	dev := "dev-noop"
	st := modem.ModemState{
		DeviceID: dev, Model: "EC20F",
		SIM: &modem.SimState{ICCID: "111", OperatorName: "OP"},
	}
	b.dispatchEvent(modem.Event{Kind: modem.EventModemAdded, DeviceID: dev, Payload: st})
	for i := 0; i < 3; i++ {
		b.dispatchEvent(modem.Event{Kind: modem.EventModemUpdated, DeviceID: dev, Payload: st})
	}
	if fb.sentCount() != 1 {
		t.Errorf("expected only 1 push (online), got %d", fb.sentCount())
	}
}

// TestPushModemRemoved_ClearsSnapshot: 拔模块再插上不该误报 SIM 替换。
func TestPushModemRemoved_ClearsSnapshot(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)
	dev := "dev-cycle"
	stOld := modem.ModemState{DeviceID: dev, Model: "EC20F",
		SIM: &modem.SimState{ICCID: "111", OperatorName: "A"}}
	stNew := modem.ModemState{DeviceID: dev, Model: "EC20F",
		SIM: &modem.SimState{ICCID: "222", OperatorName: "B"}}

	b.dispatchEvent(modem.Event{Kind: modem.EventModemAdded, DeviceID: dev, Payload: stOld})
	b.dispatchEvent(modem.Event{Kind: modem.EventModemRemoved, DeviceID: dev, Payload: stOld})
	// 再次上线，新 SIM —— 应该走 ModemAdded → 上线消息，不要触发"SIM 替换"
	b.dispatchEvent(modem.Event{Kind: modem.EventModemAdded, DeviceID: dev, Payload: stNew})

	// 期望 3 条：online(old) + offline + online(new)
	if fb.sentCount() != 3 {
		t.Fatalf("expected 3 sends, got %d", fb.sentCount())
	}
	last := fb.lastMessageText()
	if !strings.Contains(last, "上线") {
		t.Errorf("last message should be online: %s", last)
	}
	if strings.Contains(last, "替换") {
		t.Errorf("should NOT report SIM replacement after re-add: %s", last)
	}
}

func TestPushUSSDUserResponse_TriggersForceReply(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)

	// 模拟 bot 已有一个进行中的 USSD session
	dev := "dev-1"
	b.sessions.Set(b.chatID, &Session{
		Kind:     SessionUSSD,
		Step:     StepAwaitText,
		DeviceID: dev,
	})

	b.dispatchEvent(modem.Event{
		Kind:     modem.EventUSSDStateChanged,
		DeviceID: dev,
		Payload: modem.USSDState{
			SessionID:      "sess-1",
			State:          "user_response",
			NetworkRequest: "Select: 1) balance 2) bundles",
		},
	})

	if fb.sentCount() != 1 {
		t.Fatalf("expected 1 send, got %d", fb.sentCount())
	}
	mc, ok := fb.sent[0].(tgbotapi.MessageConfig)
	if !ok {
		t.Fatalf("expected MessageConfig, got %T", fb.sent[0])
	}
	if _, ok := mc.ReplyMarkup.(tgbotapi.ForceReply); !ok {
		t.Errorf("expected ForceReply markup, got %T", mc.ReplyMarkup)
	}
	// session 已切换步骤
	s := b.sessions.Get(b.chatID)
	if s == nil || s.Step != StepUSSDAwaitResponse {
		t.Errorf("session should be in await_response, got %+v", s)
	}
}

func TestPushUSSDUserResponse_NoSessionNoPush(t *testing.T) {
	prov := modem.NewMockProvider(discardLogger())
	b, fb := testBot(t, prov)

	b.dispatchEvent(modem.Event{
		Kind:     modem.EventUSSDStateChanged,
		DeviceID: "dev-1",
		Payload:  modem.USSDState{SessionID: "s", State: "user_response"},
	})
	if fb.sentCount() != 0 {
		t.Errorf("no session -> no push, got %d", fb.sentCount())
	}
}

// TestRateLimiter 确保同 chatID 连续发送间隔 ≥ gap。
func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(50 * time.Millisecond)
	ctx := context.Background()
	start := time.Now()
	rl.wait(ctx, 1) // 第一次立即返回
	rl.wait(ctx, 1) // 第二次应至少等 ~50ms
	dur := time.Since(start)
	if dur < 40*time.Millisecond {
		t.Errorf("rate limiter did not wait; elapsed %v", dur)
	}
	// 不同 chatID 互不影响
	start = time.Now()
	rl.wait(ctx, 2)
	if time.Since(start) > 10*time.Millisecond {
		t.Errorf("different chat_id should not wait")
	}
}

// TestStripMarkdownV2 边界回退
func TestStripMarkdownV2(t *testing.T) {
	if stripMarkdownV2(`a\.b`) != "a.b" {
		t.Fail()
	}
}
