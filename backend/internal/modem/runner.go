package modem

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Runner 把 Provider 的 Event channel 消费掉，做两件事：
//  1. 写入 DB（通过 Store）
//  2. 扇出给所有 Subscribe 的订阅者（WS hub / Telegram bot 等）
//
// 扇出策略：每个订阅者拥有独立 buffer channel，Runner 非阻塞投递；
// 订阅者 slow 时丢包并记 warn（比阻塞整个 Runner 更安全）。
type Runner struct {
	provider Provider
	store    *Store
	log      *slog.Logger

	subsMu sync.RWMutex
	subs   map[int64]*subscriber

	nextSubID atomic.Int64

	// 缓存：modem deviceID → modem id；避免每个事件都查 DB
	idCacheMu sync.RWMutex
	modemIDs  map[string]int64
	simIDs    map[string]int64 // deviceID → 当前绑定的 sim id
}

type subscriber struct {
	ch   chan Event
	drop atomic.Int64 // 丢弃计数，方便观测
}

// NewRunner 构造 Runner。
func NewRunner(provider Provider, store *Store, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.Default()
	}
	return &Runner{
		provider: provider,
		store:    store,
		log:      log,
		subs:     make(map[int64]*subscriber),
		modemIDs: make(map[string]int64),
		simIDs:   make(map[string]int64),
	}
}

// Subscribe 返回一个新的事件 channel 和 unsubscribe 函数。bufSize ≤ 0 时取 64。
func (r *Runner) Subscribe(bufSize int) (<-chan Event, func()) {
	if bufSize <= 0 {
		bufSize = 64
	}
	id := r.nextSubID.Add(1)
	sub := &subscriber{ch: make(chan Event, bufSize)}
	r.subsMu.Lock()
	r.subs[id] = sub
	r.subsMu.Unlock()
	return sub.ch, func() {
		r.subsMu.Lock()
		delete(r.subs, id)
		r.subsMu.Unlock()
		close(sub.ch)
	}
}

// Run 启动 Provider（在新 goroutine）并阻塞消费事件，直到 ctx 取消或 Provider 退出。
func (r *Runner) Run(ctx context.Context) error {
	provErr := make(chan error, 1)
	go func() {
		provErr <- r.provider.Start(ctx)
	}()

	// 启动 5 秒后做一次"幽灵清扫"：把 DB 里 present=1 但这次启动后未见过的
	// device_id 行标记为 present=0。修复历史遗留（例如上次运行时 present=1，
	// 用户关机后换 USB 口再开机，老 device_id 的那条行从未被本次运行触及）。
	// 迁移 0003 合并了同 IMEI 的重复行，此处处理的是 IMEI 不同 / DB 里孤立的幽灵。
	sweepTimer := time.NewTimer(5 * time.Second)
	defer sweepTimer.Stop()

	events := r.provider.Events()
	for {
		select {
		case <-ctx.Done():
			// 等 provider 也退出
			select {
			case err := <-provErr:
				if err != nil && !errors.Is(err, context.Canceled) {
					return err
				}
			case <-time.After(3 * time.Second):
				r.log.Warn("provider did not exit in time")
			}
			return nil
		case err := <-provErr:
			if err != nil {
				return err
			}
			return nil
		case <-sweepTimer.C:
			r.sweepStaleModems(ctx)
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			r.handle(ctx, ev)
			r.fanout(ev)
		}
	}
}

// sweepStaleModems 把当前 provider.ListModems() 的所有 device_id 收集起来，
// 交给 store.MarkStaleModemsAbsent 清理孤立幽灵。
// provider 尚未识别出任何模块时（device_id 列表为空）直接跳过，避免误清全库。
func (r *Runner) sweepStaleModems(ctx context.Context) {
	snapshots := r.provider.ListModems()
	if len(snapshots) == 0 {
		r.log.Debug("stale sweep skipped: no modems seen yet")
		return
	}
	ids := make([]string, 0, len(snapshots))
	for _, m := range snapshots {
		if m.DeviceID != "" {
			ids = append(ids, m.DeviceID)
		}
	}
	n, err := r.store.MarkStaleModemsAbsent(ctx, ids)
	if err != nil {
		r.log.Warn("stale modem sweep failed", "err", err)
		return
	}
	if n > 0 {
		r.log.Info("stale modem sweep", "seen", len(ids), "marked_absent", n)
	} else {
		r.log.Debug("stale modem sweep clean", "seen", len(ids))
	}
}

// handle 根据事件类型更新 DB。
func (r *Runner) handle(ctx context.Context, ev Event) {
	switch ev.Kind {
	case EventModemAdded, EventModemUpdated:
		state, ok := ev.Payload.(ModemState)
		if !ok {
			return
		}
		id, err := r.store.UpsertModem(ctx, state)
		if err != nil {
			r.log.Error("upsert modem failed", "device", state.DeviceID, "err", err)
			return
		}
		r.setModemID(state.DeviceID, id)
		if state.SIM != nil {
			if simID, err := r.store.UpsertSim(ctx, *state.SIM, id); err != nil {
				r.log.Error("upsert sim failed", "device", state.DeviceID, "err", err)
			} else if simID > 0 {
				r.setSimID(state.DeviceID, simID)
			}
		} else {
			// SIM 被拔出 / 模块 failed sim-missing：清理 modem↔sim 绑定，
			// 避免前端 /api/modems 继续 join 出已经不在场的 SIM。
			if err := r.store.UnbindModem(ctx, id); err != nil {
				r.log.Debug("unbind modem sim failed", "device", state.DeviceID, "err", err)
			}
			r.idCacheMu.Lock()
			delete(r.simIDs, state.DeviceID)
			r.idCacheMu.Unlock()
		}
		if ev.Kind == EventModemAdded {
			r.log.Info("modem online",
				"device", state.DeviceID, "model", state.Model,
				"state", state.State, "iccid", iccidOf(state.SIM))
		}

	case EventModemRemoved:
		state, _ := ev.Payload.(ModemState)
		if err := r.store.MarkModemAbsent(ctx, state.DeviceID); err != nil {
			r.log.Error("mark modem absent failed", "device", state.DeviceID, "err", err)
		}
		// 清掉 modem↔sim 绑定，不残留在前端视图里。
		if id := r.getModemID(ctx, state.DeviceID); id > 0 {
			if err := r.store.UnbindModem(ctx, id); err != nil {
				r.log.Debug("unbind on remove failed", "device", state.DeviceID, "err", err)
			}
		}
		r.forgetModem(state.DeviceID)
		r.log.Info("modem offline", "device", state.DeviceID)

	case EventSimUpdated:
		sim, ok := ev.Payload.(SimState)
		if !ok {
			return
		}
		modemID := r.getModemID(ctx, ev.DeviceID)
		if simID, err := r.store.UpsertSim(ctx, sim, modemID); err != nil {
			r.log.Error("upsert sim failed", "device", ev.DeviceID, "err", err)
		} else if simID > 0 {
			r.setSimID(ev.DeviceID, simID)
		}

	case EventSignalSampled:
		sample, ok := ev.Payload.(SignalSample)
		if !ok {
			return
		}
		modemID := r.getModemID(ctx, ev.DeviceID)
		simID := r.getSimID(ctx, ev.DeviceID, modemID)
		if modemID == 0 {
			return // 还没见过该 modem
		}
		if err := r.store.InsertSignalSample(ctx, modemID, simID, sample); err != nil {
			r.log.Debug("insert signal sample failed", "err", err)
		}

	case EventSMSReceived:
		rec, ok := ev.Payload.(SMSRecord)
		if !ok {
			return
		}
		modemID := r.getModemID(ctx, ev.DeviceID)
		simID := r.getSimID(ctx, ev.DeviceID, modemID)
		if err := r.store.InsertSMS(ctx, rec, modemID, simID); err != nil {
			r.log.Error("insert sms failed", "err", err)
			return
		}
		r.log.Info("sms received",
			"device", ev.DeviceID, "peer", rec.Peer,
			"len", len(rec.Text))

	case EventSMSStateChanged:
		rec, ok := ev.Payload.(SMSRecord)
		if !ok {
			return
		}
		modemID := r.getModemID(ctx, ev.DeviceID)
		simID := r.getSimID(ctx, ev.DeviceID, modemID)
		// 对于 outbound，我们在第一次出现时也要插入
		if err := r.store.InsertSMS(ctx, rec, modemID, simID); err != nil {
			r.log.Debug("upsert sms failed", "err", err)
		}
		_ = r.store.UpdateSMSState(ctx, rec.ExtID, rec.State)

	case EventUSSDStateChanged:
		u, ok := ev.Payload.(USSDState)
		if !ok {
			return
		}
		modemID := r.getModemID(ctx, ev.DeviceID)
		if modemID == 0 {
			return
		}
		if u.LastRequest != "" {
			_ = r.store.AppendUSSD(ctx, u.SessionID, "out", u.LastRequest, modemID)
		}
		if u.LastResponse != "" {
			_ = r.store.AppendUSSD(ctx, u.SessionID, "in", u.LastResponse, modemID)
		}
		if u.NetworkRequest != "" {
			_ = r.store.AppendUSSD(ctx, u.SessionID, "in", u.NetworkRequest, modemID)
		}
		if u.NetworkNotification != "" {
			_ = r.store.AppendUSSD(ctx, u.SessionID, "notification", u.NetworkNotification, modemID)
		}
		_ = r.store.SetUSSDState(ctx, modemID, mapUSSDDBState(u.State))
		r.log.Info("ussd state", "device", ev.DeviceID, "state", u.State)
	}
}

// fanout 非阻塞地把事件推给所有订阅者。
func (r *Runner) fanout(ev Event) {
	r.subsMu.RLock()
	defer r.subsMu.RUnlock()
	for id, sub := range r.subs {
		select {
		case sub.ch <- ev:
		default:
			n := sub.drop.Add(1)
			if n%100 == 1 {
				r.log.Warn("subscriber slow, dropping event",
					"sub_id", id, "kind", ev.Kind, "dropped_total", n)
			}
		}
	}
}

// ---------- ID 缓存辅助 ----------

func (r *Runner) setModemID(deviceID string, id int64) {
	r.idCacheMu.Lock()
	r.modemIDs[deviceID] = id
	r.idCacheMu.Unlock()
}

func (r *Runner) setSimID(deviceID string, id int64) {
	r.idCacheMu.Lock()
	r.simIDs[deviceID] = id
	r.idCacheMu.Unlock()
}

func (r *Runner) forgetModem(deviceID string) {
	r.idCacheMu.Lock()
	delete(r.modemIDs, deviceID)
	delete(r.simIDs, deviceID)
	r.idCacheMu.Unlock()
}

func (r *Runner) getModemID(ctx context.Context, deviceID string) int64 {
	r.idCacheMu.RLock()
	id, ok := r.modemIDs[deviceID]
	r.idCacheMu.RUnlock()
	if ok {
		return id
	}
	id, err := r.store.ModemIDByDevice(ctx, deviceID)
	if err != nil {
		return 0
	}
	r.setModemID(deviceID, id)
	return id
}

func (r *Runner) getSimID(ctx context.Context, deviceID string, modemID int64) int64 {
	r.idCacheMu.RLock()
	id, ok := r.simIDs[deviceID]
	r.idCacheMu.RUnlock()
	if ok {
		return id
	}
	if modemID == 0 {
		return 0
	}
	id, err := r.store.SimIDByModem(ctx, modemID)
	if err != nil || id == 0 {
		return 0
	}
	r.setSimID(deviceID, id)
	return id
}

func iccidOf(s *SimState) string {
	if s == nil {
		return ""
	}
	return s.ICCID
}

// mapUSSDDBState 把 provider 的语义状态转为 DB schema 用的枚举。
func mapUSSDDBState(s string) string {
	switch s {
	case "idle":
		return "terminated"
	case "active":
		return "active"
	case "user_response":
		return "user_response"
	case "unknown":
		return "failed"
	default:
		return "active"
	}
}
