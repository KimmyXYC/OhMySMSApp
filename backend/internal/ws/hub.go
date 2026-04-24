// Package ws 提供 WebSocket hub：把 modem.Runner 的事件广播给所有活跃连接。
//
// 设计：
//   - Hub 持有所有 conn，Run(ctx) 消费 runner.Subscribe() 返回的事件 channel 并 fanout
//   - 每个 conn 一个出站 buffer channel；满了直接丢并累计统计
//   - 入站只处理 ping / auth（未来可加 subscribe 过滤）
//   - 心跳：每 30s 发一次 WS ping frame（由 coder/websocket 自动处理），连续 2 次超时断开
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	cws "github.com/coder/websocket"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/auth"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// Hub 把 runner 事件扇出到 WS 客户端。
type Hub struct {
	log    *slog.Logger
	runner *modem.Runner
	auth   *auth.Service // 可为 nil，此时不做鉴权（仅测试）

	mu     sync.RWMutex
	conns  map[int64]*Conn
	nextID atomic.Int64

	allowedOrigins []string // Origin 白名单；空=不校验 Origin（同源）

	// 订阅 runner 事件的 unsubscribe 函数
	unsub func()
}

// NewHub 构造 Hub。allowedOrigins 为 nil/空时表示不做 Origin 校验。
func NewHub(runner *modem.Runner, svc *auth.Service, log *slog.Logger, allowedOrigins []string) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		log:            log,
		runner:         runner,
		auth:           svc,
		conns:          make(map[int64]*Conn),
		allowedOrigins: allowedOrigins,
	}
}

// Run 在后台订阅 runner 事件并广播。阻塞直到 ctx 取消。
func (h *Hub) Run(ctx context.Context) {
	events, unsub := h.runner.Subscribe(256)
	h.unsub = unsub
	defer unsub()

	h.log.Info("ws hub started")
	for {
		select {
		case <-ctx.Done():
			h.closeAll("shutdown")
			h.log.Info("ws hub stopped")
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			h.dispatch(ev)
		}
	}
}

// dispatch 将一个 modem.Event 转成 ws 消息并广播。
func (h *Hub) dispatch(ev modem.Event) {
	msg, ok := toWSMessage(ev)
	if !ok {
		return
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		h.log.Warn("ws marshal failed", "err", err, "type", msg.Type)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.conns {
		c.send(raw)
	}
}

// NumConns 活跃连接数（用于观测）。
func (h *Hub) NumConns() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}

// register / unregister 由 Conn 调用。
func (h *Hub) register(c *Conn) {
	h.mu.Lock()
	h.conns[c.id] = c
	h.mu.Unlock()
}

func (h *Hub) unregister(c *Conn) {
	h.mu.Lock()
	delete(h.conns, c.id)
	h.mu.Unlock()
}

func (h *Hub) closeAll(reason string) {
	h.mu.Lock()
	conns := make([]*Conn, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		c.closeWith(cws.StatusGoingAway, reason)
	}
}

// ServeHTTP 把 HTTP 请求升级为 WS 并加入 hub。
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 鉴权：优先 Authorization header，再看 ?token=
	if h.auth != nil {
		if _, err := h.auth.ValidateQueryToken(r); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`","code":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	opts := &cws.AcceptOptions{}
	if len(h.allowedOrigins) > 0 {
		opts.OriginPatterns = h.allowedOrigins
	} else {
		// 不配置白名单时允许跨域（开发场景 vite dev → backend）
		opts.InsecureSkipVerify = true
	}

	c, err := cws.Accept(w, r, opts)
	if err != nil {
		h.log.Warn("ws accept failed", "err", err, "remote", r.RemoteAddr)
		return
	}
	// 限制读消息大小
	c.SetReadLimit(32 * 1024)

	ctx := r.Context()
	id := h.nextID.Add(1)
	conn := &Conn{
		id:   id,
		hub:  h,
		ws:   c,
		out:  make(chan []byte, 64),
		log:  h.log.With("conn_id", id, "remote", r.RemoteAddr),
		done: make(chan struct{}),
	}
	h.register(conn)
	h.log.Info("ws client connected", "conn_id", id, "remote", r.RemoteAddr, "total", h.NumConns())
	conn.serve(ctx)
	h.unregister(conn)
	h.log.Info("ws client disconnected", "conn_id", id, "total", h.NumConns())
}

// dispatchCtxTimeout 单个 write 的超时。
const writeTimeout = 10 * time.Second
