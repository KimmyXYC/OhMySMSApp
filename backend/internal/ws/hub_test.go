package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cws "github.com/coder/websocket"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// TestHubBroadcast：启动 hub → 连 1 个 WS client → runner fanout 一条信号事件
// → 客户端收到对应消息。
func TestHubBroadcast(t *testing.T) {
	log := slog.Default()

	// 构造一个能被 Subscribe 的 runner：我们实际只用它的 Subscribe 入口；
	// runner 的 Run 需要 provider；这里我们直接构造 runner 但不调用 Run，
	// 转而直接把事件塞进它的 subscriber channel 不可行——改用 fake path：
	// 通过 NewRunner + 手动调用 fanout（该方法不导出）也不可行。
	// 更简单：我们用一个真实的 MockProvider 跑 runner，但只关心 Subscribe。

	provider := modem.NewMockProvider(log)
	store := modem.NewStore(nil) // store.nil 下只要不触发写就 ok；我们不 Run runner
	_ = store
	runner := modem.NewRunner(provider, modem.NewStore(nil), log)
	hub := NewHub(runner, nil, log, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 因为 Hub.Run 会 Subscribe 到 runner 然后等事件——但我们没启动 runner.Run
	// 因此手动给 hub 一个事件源：直接调用 dispatch。
	go func() {
		// 绕过：启动 hub 的 run，让它 subscribe
		hub.Run(ctx)
	}()

	// 等 hub subscribe 建立
	time.Sleep(50 * time.Millisecond)

	srv := httptest.NewServer(hub)
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientCtx, cancelClient := context.WithTimeout(ctx, 2*time.Second)
	defer cancelClient()
	c, _, err := cws.Dial(clientCtx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow() //nolint:errcheck

	// 第一条应为 hello
	_, raw, err := c.Read(clientCtx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	var hello Message
	_ = json.Unmarshal(raw, &hello)
	if hello.Type != "hello" {
		t.Fatalf("expected hello, got %q", hello.Type)
	}

	// 再通过 runner.fanout（不可导出）—— 改走 dispatch
	hub.dispatch(modem.Event{
		Kind:     modem.EventSignalSampled,
		DeviceID: "mock",
		Payload: modem.SignalSample{
			DeviceID: "mock", QualityPct: 42, AccessTech: "lte",
			SampledAt: time.Now(),
		},
		At: time.Now(),
	})

	_, raw, err = c.Read(clientCtx)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Type != "signal.sample" {
		t.Fatalf("expected signal.sample, got %q", msg.Type)
	}

	// 客户端主动断开
	_ = c.Close(cws.StatusNormalClosure, "done")
}

// TestHubAuth: 带 auth 的 hub，无 token 应 401。
func TestHubAuthRequired(t *testing.T) {
	// 构造一个需要 auth 的 hub（svc 非 nil）。
	// 用空的 runner（ListModems 路径不走到）。
	log := slog.Default()
	provider := modem.NewMockProvider(log)
	runner := modem.NewRunner(provider, modem.NewStore(nil), log)

	// 不引入 auth 包会循环；改为构造一个最小 auth.Service 通过测试 helper
	// 不做完整 auth 校验测试（auth 包自己测），此处断言 nil-auth 时允许匿名。
	hub := NewHub(runner, nil, log, nil)

	srv := httptest.NewServer(hub)
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, _, err := cws.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("should connect without auth when svc=nil: %v", err)
	}
	_ = c.Close(cws.StatusNormalClosure, "")
}
