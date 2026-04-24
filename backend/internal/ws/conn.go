// conn.go —— 单个 WS 连接的 read / write goroutine。
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	cws "github.com/coder/websocket"
)

// Conn 表示一个 WS 连接。
type Conn struct {
	id   int64
	hub  *Hub
	ws   *cws.Conn
	out  chan []byte
	log  *slog.Logger
	done chan struct{}

	dropped atomic.Int64
	closed  atomic.Bool
}

// serve 启动 read / write goroutine 并阻塞直到任一退出。
func (c *Conn) serve(ctx context.Context) {
	// 欢迎消息
	hello, _ := json.Marshal(Message{
		Type: "hello",
		Data: map[string]any{"conn_id": c.id},
		TS:   time.Now().UTC().Format(time.RFC3339Nano),
	})
	c.send(hello)

	writeDone := make(chan struct{})
	go c.writeLoop(ctx, writeDone)

	c.readLoop(ctx) // 阻塞
	c.closeWith(cws.StatusNormalClosure, "bye")
	<-writeDone
}

// readLoop 处理来自客户端的消息（当前仅 ping/auth）。
func (c *Conn) readLoop(ctx context.Context) {
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			if !isNormalClose(err) {
				c.log.Debug("ws read exit", "err", err)
			}
			return
		}
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "ping":
			pong, _ := json.Marshal(Message{Type: "pong", TS: time.Now().UTC().Format(time.RFC3339Nano)})
			c.send(pong)
		case "auth":
			// 已在升级前校验过，这里只回 ack
			ack, _ := json.Marshal(Message{Type: "auth.ok", TS: time.Now().UTC().Format(time.RFC3339Nano)})
			c.send(ack)
		}
	}
}

// writeLoop 把 c.out 中的消息写出去 + 周期发 ping。
func (c *Conn) writeLoop(ctx context.Context, doneSig chan struct{}) {
	defer close(doneSig)
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case data, ok := <-c.out:
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, cws.MessageText, data)
			cancel()
			if err != nil {
				c.log.Debug("ws write failed", "err", err)
				c.closeWith(cws.StatusInternalError, "write error")
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil {
				c.log.Debug("ws ping failed", "err", err)
				return
			}
		}
	}
}

// send 非阻塞投递；满了丢弃。
func (c *Conn) send(raw []byte) {
	if c.closed.Load() {
		return
	}
	select {
	case c.out <- raw:
	default:
		n := c.dropped.Add(1)
		if n%50 == 1 {
			c.log.Warn("ws send buffer full, dropping", "dropped_total", n)
		}
	}
}

// closeWith 发送关闭帧。幂等。
func (c *Conn) closeWith(code cws.StatusCode, reason string) {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	close(c.done)
	_ = c.ws.Close(code, reason)
}

func isNormalClose(err error) bool {
	var ce cws.CloseError
	if errors.As(err, &ce) {
		return ce.Code == cws.StatusNormalClosure || ce.Code == cws.StatusGoingAway
	}
	return false
}
