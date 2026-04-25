package esim

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// inhibitor 通过 ModemManager DBus 接口对一个 modem 的 sysfs uid（Modem.Device 属性，
// 通常等同于 sysfs 路径，例如 "/sys/devices/.../usb1/1-1/...:1.5"）
// 调用 InhibitDevice(uid, inhibit) 暂停 / 恢复 MM 对该 modem 的占用。
//
// 设计：
//   - 每次 enable/disable 流程是一对 inhibit(true) → defer inhibit(false)。
//   - 进程内幂等：同一个 uid 同时只允许一个 active inhibit；二次 inhibit 会被忽略。
//   - DBus 连接复用 system bus；连接失败重连一次。
//
// 备注：Modem.Device 属性（DBus 上的字符串）就是 InhibitDevice 期望的 uid，
// 详见 ModemManager API 文档：
// org.freedesktop.ModemManager1.InhibitDevice(in s uid, in b inhibit)
type inhibitor struct {
	mu     sync.Mutex
	conn   *dbus.Conn
	active map[string]int // uid → 引用计数，方便嵌套调用
}

func newInhibitor() *inhibitor {
	return &inhibitor{active: make(map[string]int)}
}

// connect 取一个可用的 system bus 连接（懒加载 + 自动重连）。
func (i *inhibitor) connect() (*dbus.Conn, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.conn != nil && i.conn.Connected() {
		return i.conn, nil
	}
	c, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}
	i.conn = c
	return c, nil
}

// inhibit 调用 InhibitDevice(uid, true)。返回时 MM 已经放手该 modem。
//
// 当 ctx 超时仍未拿到 DBus 应答时返回包装后的 ErrInhibitFailed。
// 多次对同一 uid 调用会做引用计数；只有第一次真正发 DBus 请求。
func (i *inhibitor) inhibit(ctx context.Context, uid string) error {
	if uid == "" {
		return errors.New("inhibit: empty uid")
	}
	i.mu.Lock()
	if i.active[uid] > 0 {
		i.active[uid]++
		i.mu.Unlock()
		return nil
	}
	i.mu.Unlock()

	conn, err := i.connect()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInhibitFailed, err)
	}
	obj := conn.Object(
		"org.freedesktop.ModemManager1",
		dbus.ObjectPath("/org/freedesktop/ModemManager1"),
	)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	call := obj.CallWithContext(cctx,
		"org.freedesktop.ModemManager1.InhibitDevice", 0, uid, true)
	if call.Err != nil {
		return fmt.Errorf("%w: %v", ErrInhibitFailed, call.Err)
	}
	i.mu.Lock()
	i.active[uid]++
	i.mu.Unlock()
	return nil
}

// uninhibit 释放一次 inhibit。引用计数归零时才发 InhibitDevice(uid, false)。
// 失败时只返回 error；调用方一般用 defer，可忽略。
func (i *inhibitor) uninhibit(ctx context.Context, uid string) error {
	if uid == "" {
		return nil
	}
	i.mu.Lock()
	cnt := i.active[uid]
	if cnt <= 0 {
		i.mu.Unlock()
		return nil
	}
	i.active[uid] = cnt - 1
	if cnt > 1 {
		i.mu.Unlock()
		return nil
	}
	delete(i.active, uid)
	i.mu.Unlock()

	conn, err := i.connect()
	if err != nil {
		return fmt.Errorf("uninhibit: %w", err)
	}
	obj := conn.Object(
		"org.freedesktop.ModemManager1",
		dbus.ObjectPath("/org/freedesktop/ModemManager1"),
	)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	call := obj.CallWithContext(cctx,
		"org.freedesktop.ModemManager1.InhibitDevice", 0, uid, false)
	if call.Err != nil {
		return fmt.Errorf("uninhibit: %w", call.Err)
	}
	return nil
}

// close 关闭复用连接（仅在 Service 终止时调用）。
func (i *inhibitor) close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.conn != nil {
		_ = i.conn.Close()
		i.conn = nil
	}
}
