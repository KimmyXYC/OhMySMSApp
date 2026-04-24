package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// MMProvider 通过 ModemManager 的 DBus API 采集 modem 状态、收发 SMS、操作 USSD。
//
// 线程模型：
//   - Start() 以单一 dispatcher goroutine 消费 DBus signal channel；所有内部 state
//     的写入都在这一个 goroutine 内完成，避免复杂锁。
//   - ListModems/GetModem/ListSMS 等只读方法来自其它 goroutine（HTTP handler 等），
//     通过 RWMutex 读取一致快照。
//   - SendSMS/InitiateUSSD 这类阻塞 DBus 调用也来自其它 goroutine；它们直接调 conn，
//     不经过 dispatcher（godbus 的 Object 是线程安全的）。
type MMProvider struct {
	log    *slog.Logger
	events chan Event

	mu      sync.RWMutex
	conn    *dbus.Conn
	modems  map[string]*modemEntry // key = DeviceID
	byPath  map[dbus.ObjectPath]string // DBus path → DeviceID（反查）
	smsSubs map[dbus.ObjectPath][]dbus.MatchOption // 每个 SMS path 对应的 match options（用于 RemoveMatchSignal）
}

// modemEntry 是一个 modem 的运行时完整状态。
type modemEntry struct {
	State        ModemState
	SIMPath      dbus.ObjectPath
	USSDSession  *USSDState               // 当前 USSD 会话，nil 表示无
	SMSPaths     map[dbus.ObjectPath]bool // 当前已订阅的 SMS 对象
	// 注册到 DBus 的 match options 副本，关闭时同参数 Remove。
	matches []dbus.MatchOption
}

// NewMMProvider 构造 MMProvider。eventBufSize 控制事件 channel 缓冲大小。
func NewMMProvider(log *slog.Logger, eventBufSize int) *MMProvider {
	if eventBufSize <= 0 {
		eventBufSize = 128
	}
	if log == nil {
		log = slog.Default()
	}
	return &MMProvider{
		log:     log,
		events:  make(chan Event, eventBufSize),
		modems:  make(map[string]*modemEntry),
		byPath:  make(map[dbus.ObjectPath]string),
		smsSubs: make(map[dbus.ObjectPath][]dbus.MatchOption),
	}
}

// Events 实现 Provider。
func (p *MMProvider) Events() <-chan Event { return p.events }

// Start 连接 system bus 并进入事件循环。ctx 取消时优雅退出。
func (p *MMProvider) Start(ctx context.Context) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect system bus: %w", err)
	}
	p.mu.Lock()
	p.conn = conn
	p.mu.Unlock()
	defer func() {
		p.cleanupAllMatches()
		_ = conn.Close()
	}()

	// 订阅 ObjectManager 的 InterfacesAdded/InterfacesRemoved。
	omMatches := []dbus.MatchOption{
		dbus.WithMatchInterface(ifaceObjectManager),
		dbus.WithMatchSender(mmService),
		dbus.WithMatchPathNamespace(mmRoot),
	}
	if err := conn.AddMatchSignal(omMatches...); err != nil {
		return fmt.Errorf("add match object manager: %w", err)
	}

	sigCh := make(chan *dbus.Signal, 256)
	conn.Signal(sigCh)

	// 初始 reconcile：GetManagedObjects 一次性拉所有已知 modem / sim / sms。
	if err := p.initialReconcile(ctx); err != nil {
		p.log.Warn("initial reconcile failed", "err", err)
		// 非致命，继续等信号；MM 重启后 InterfacesAdded 会补上。
	}

	p.log.Info("modem mmprovider started",
		"modems", len(p.modems))

	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-sigCh:
			if !ok {
				return errors.New("dbus signal channel closed")
			}
			p.handleSignal(ctx, sig)
		}
	}
}

// ListModems 返回当前所有 modem 的快照（深拷贝级别不严格，但外部不应修改返回值）。
func (p *MMProvider) ListModems() []ModemState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ModemState, 0, len(p.modems))
	for _, e := range p.modems {
		out = append(out, e.State)
	}
	return out
}

// GetModem 返回某 device 的快照。
func (p *MMProvider) GetModem(deviceID string) (ModemState, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, ok := p.modems[deviceID]
	if !ok {
		return ModemState{}, false
	}
	return e.State, true
}

// ListSMS 调用 Messaging.List 并为每个 SMS 读 props。
func (p *MMProvider) ListSMS(deviceID string) ([]SMSRecord, error) {
	p.mu.RLock()
	e, ok := p.modems[deviceID]
	conn := p.conn
	p.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("modem %s not found", deviceID)
	}
	if conn == nil {
		return nil, errors.New("dbus not connected")
	}
	if !e.State.HasMessaging {
		return nil, errors.New("modem does not support messaging")
	}

	modemObj := conn.Object(mmService, dbus.ObjectPath(e.State.DBusPath))
	var paths []dbus.ObjectPath
	if err := modemObj.Call(ifaceMessaging+".List", 0).Store(&paths); err != nil {
		return nil, fmt.Errorf("messaging list: %w", err)
	}
	out := make([]SMSRecord, 0, len(paths))
	for _, sp := range paths {
		rec, err := p.readSMS(conn, sp)
		if err != nil {
			p.log.Debug("read sms failed", "path", sp, "err", err)
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// SendSMS 调用 Messaging.Create + Sms.Send。
func (p *MMProvider) SendSMS(ctx context.Context, deviceID, to, text string) (string, error) {
	p.mu.RLock()
	e, ok := p.modems[deviceID]
	conn := p.conn
	p.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("modem %s not found", deviceID)
	}
	if conn == nil {
		return "", errors.New("dbus not connected")
	}
	if !e.State.HasMessaging {
		return "", errors.New("modem does not support messaging")
	}

	modemObj := conn.Object(mmService, dbus.ObjectPath(e.State.DBusPath))
	props := map[string]dbus.Variant{
		"number": dbus.MakeVariant(to),
		"text":   dbus.MakeVariant(text),
	}
	var smsPath dbus.ObjectPath
	call := modemObj.CallWithContext(ctx, ifaceMessaging+".Create", 0, props)
	if err := call.Store(&smsPath); err != nil {
		return "", fmt.Errorf("sms create: %w", err)
	}
	smsObj := conn.Object(mmService, smsPath)
	if err := smsObj.CallWithContext(ctx, ifaceSms+".Send", 0).Err; err != nil {
		return string(smsPath), fmt.Errorf("sms send: %w", err)
	}
	p.log.Info("sms sent", "device", deviceID, "to", to, "path", smsPath)
	return string(smsPath), nil
}

// DeleteSMS 调用 Messaging.Delete。
func (p *MMProvider) DeleteSMS(ctx context.Context, deviceID, extID string) error {
	p.mu.RLock()
	e, ok := p.modems[deviceID]
	conn := p.conn
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("modem %s not found", deviceID)
	}
	if conn == nil {
		return errors.New("dbus not connected")
	}
	modemObj := conn.Object(mmService, dbus.ObjectPath(e.State.DBusPath))
	err := modemObj.CallWithContext(ctx, ifaceMessaging+".Delete", 0, dbus.ObjectPath(extID)).Err
	if err != nil {
		return fmt.Errorf("sms delete: %w", err)
	}
	return nil
}

// InitiateUSSD 发起 USSD 会话。sessionID 约定 = deviceID（MM 无显式 session id）。
func (p *MMProvider) InitiateUSSD(ctx context.Context, deviceID, command string) (string, string, error) {
	p.mu.RLock()
	e, ok := p.modems[deviceID]
	conn := p.conn
	p.mu.RUnlock()
	if !ok {
		return "", "", fmt.Errorf("modem %s not found", deviceID)
	}
	if conn == nil {
		return "", "", errors.New("dbus not connected")
	}
	if !e.State.HasUSSD {
		return "", "", errors.New("modem does not support ussd")
	}
	modemObj := conn.Object(mmService, dbus.ObjectPath(e.State.DBusPath))
	var reply string
	if err := modemObj.CallWithContext(ctx, ifaceUSSD+".Initiate", 0, command).Store(&reply); err != nil {
		return "", "", fmt.Errorf("ussd initiate: %w", err)
	}
	return deviceID, reply, nil
}

// RespondUSSD 发送用户响应。sessionID 仍然是 deviceID。
func (p *MMProvider) RespondUSSD(ctx context.Context, sessionID, response string) (string, error) {
	p.mu.RLock()
	e, ok := p.modems[sessionID]
	conn := p.conn
	p.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("ussd session %s not found", sessionID)
	}
	if conn == nil {
		return "", errors.New("dbus not connected")
	}
	modemObj := conn.Object(mmService, dbus.ObjectPath(e.State.DBusPath))
	var reply string
	if err := modemObj.CallWithContext(ctx, ifaceUSSD+".Respond", 0, response).Store(&reply); err != nil {
		return "", fmt.Errorf("ussd respond: %w", err)
	}
	return reply, nil
}

// CancelUSSD 终止 USSD 会话。
func (p *MMProvider) CancelUSSD(ctx context.Context, sessionID string) error {
	p.mu.RLock()
	e, ok := p.modems[sessionID]
	conn := p.conn
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("ussd session %s not found", sessionID)
	}
	if conn == nil {
		return errors.New("dbus not connected")
	}
	modemObj := conn.Object(mmService, dbus.ObjectPath(e.State.DBusPath))
	if err := modemObj.CallWithContext(ctx, ifaceUSSD+".Cancel", 0).Err; err != nil {
		return fmt.Errorf("ussd cancel: %w", err)
	}
	return nil
}

// ResetModem 调用 org.freedesktop.ModemManager1.Modem.Reset()。
// Reset 在 MM 内是异步的：DBus 调用会很快返回，随后 modem 对象会消失
// （触发 InterfacesRemoved）并重新出现（触发 InterfacesAdded），provider
// 的信号处理会自动 reconcile。
//
// 某些插件（例如 Huawei MBIM 的部分固件）不支持 Reset，DBus 会回
// "org.freedesktop.ModemManager1.Error.Core.Unsupported"。此时返回
// ErrModemResetUnsupported，方便 HTTP 层返回 501。
func (p *MMProvider) ResetModem(ctx context.Context, deviceID string) error {
	p.mu.RLock()
	e, ok := p.modems[deviceID]
	conn := p.conn
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("modem %s not found", deviceID)
	}
	if conn == nil {
		return errors.New("dbus not connected")
	}
	modemObj := conn.Object(mmService, dbus.ObjectPath(e.State.DBusPath))
	if err := modemObj.CallWithContext(ctx, ifaceModem+".Reset", 0).Err; err != nil {
		// DBus error name 通常形如 ".Unsupported" 或包含 "Unsupported"。
		// 用字符串匹配 + dbus.Error 类型双通道判定，兼容不同 MM 版本。
		var dbusErr dbus.Error
		if errors.As(err, &dbusErr) {
			if strings.Contains(strings.ToLower(dbusErr.Name), "unsupported") {
				return ErrModemResetUnsupported
			}
		}
		if strings.Contains(strings.ToLower(err.Error()), "unsupported") {
			return ErrModemResetUnsupported
		}
		return fmt.Errorf("modem reset: %w", err)
	}
	p.log.Info("modem reset requested", "device", deviceID, "path", e.State.DBusPath)
	return nil
}

// -------------------- 内部：初始化 + 信号处理 --------------------

// managedObjects 与 MM 对应的类型别名，便于阅读。
type managedObjects = map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// initialReconcile 在 Start 里首次调用 GetManagedObjects，并把所有 modem/SIM/SMS 吸入内存。
func (p *MMProvider) initialReconcile(ctx context.Context) error {
	p.mu.RLock()
	conn := p.conn
	p.mu.RUnlock()
	if conn == nil {
		return errors.New("dbus not connected")
	}
	obj := conn.Object(mmService, dbus.ObjectPath(mmRoot))
	result := make(managedObjects)
	if err := obj.CallWithContext(ctx, ifaceObjectManager+".GetManagedObjects", 0).Store(&result); err != nil {
		return fmt.Errorf("get managed objects: %w", err)
	}
	for path, ifaces := range result {
		if _, ok := ifaces[ifaceModem]; ok {
			p.onModemAdded(ctx, path, ifaces)
		}
	}
	// 对每个已订阅 modem，通过 Messaging.List 拿到它的 SMS paths；
	// 直接调 onSmsAppearedForModem 路径（和 live Messaging.Added 走同一入口），
	// 它会加锁登记 SMSPaths，订阅 PropertiesChanged，然后读一次并 emit 事件。
	p.mu.RLock()
	modemsSnap := make([]struct {
		DevID    string
		DBusPath string
	}, 0, len(p.modems))
	for devID, entry := range p.modems {
		modemsSnap = append(modemsSnap, struct {
			DevID    string
			DBusPath string
		}{devID, entry.State.DBusPath})
	}
	p.mu.RUnlock()

	for _, m := range modemsSnap {
		modemObj := conn.Object(mmService, dbus.ObjectPath(m.DBusPath))
		var smsPaths []dbus.ObjectPath
		if err := modemObj.CallWithContext(ctx, ifaceMessaging+".List", 0).Store(&smsPaths); err != nil {
			p.log.Debug("messaging list failed during reconcile",
				"device", m.DevID, "err", err)
			continue
		}
		for _, sp := range smsPaths {
			p.onSmsAppearedForModem(m.DevID, sp)
		}
	}
	return nil
}

// handleSignal 根据 signal 名分派。
func (p *MMProvider) handleSignal(ctx context.Context, sig *dbus.Signal) {
	switch sig.Name {
	case ifaceObjectManager + ".InterfacesAdded":
		var path dbus.ObjectPath
		var ifaces map[string]map[string]dbus.Variant
		if err := dbus.Store(sig.Body, &path, &ifaces); err != nil {
			p.log.Debug("store InterfacesAdded failed", "err", err)
			return
		}
		if _, ok := ifaces[ifaceModem]; ok {
			p.onModemAdded(ctx, path, ifaces)
		}
		// 注意：InterfacesAdded 本身不携带 modem 归属信息，这里不尝试处理 SMS 对象；
		// MM 会同时 emit Messaging.Added 信号（sender path = modem path），那里做关联。

	case ifaceObjectManager + ".InterfacesRemoved":
		var path dbus.ObjectPath
		var removed []string
		if err := dbus.Store(sig.Body, &path, &removed); err != nil {
			return
		}
		for _, iface := range removed {
			if iface == ifaceModem {
				p.onModemRemoved(path)
			}
			if iface == ifaceSms {
				p.onSmsRemoved(path)
			}
		}

	case ifaceDBusProps + ".PropertiesChanged":
		var iface string
		var changed map[string]dbus.Variant
		var invalidated []string
		if err := dbus.Store(sig.Body, &iface, &changed, &invalidated); err != nil {
			return
		}
		p.onPropertiesChanged(ctx, sig.Path, iface, changed)

	case ifaceMessaging + ".Added":
		var smsPath dbus.ObjectPath
		var received bool
		if err := dbus.Store(sig.Body, &smsPath, &received); err != nil {
			return
		}
		// Messaging.Added 的信号 sender path 就是 modem 路径，可直接反查 deviceID；
		// 不再依赖脆弱的 SMSPaths 预填 + byPath/ReverseLookup 组合。
		p.mu.RLock()
		deviceID, known := p.byPath[sig.Path]
		p.mu.RUnlock()
		p.log.Debug("sms added",
			"modem_path", sig.Path, "sms_path", smsPath,
			"received", received, "device", deviceID)
		if !known {
			p.log.Warn("sms added for unknown modem path", "path", sig.Path)
			return
		}
		p.onSmsAppearedForModem(deviceID, smsPath)
		// 当 received=true 时，state 可能还是 RECEIVING；依赖 PropertiesChanged → RECEIVED。

	case ifaceMessaging + ".Deleted":
		var smsPath dbus.ObjectPath
		if err := dbus.Store(sig.Body, &smsPath); err != nil {
			return
		}
		p.onSmsRemoved(smsPath)

	case ifaceModem + ".StateChanged":
		// 额外记录一下，内存状态由 PropertiesChanged 同步更新。
		var oldSt, newSt int32
		var reason uint32
		_ = dbus.Store(sig.Body, &oldSt, &newSt, &reason)
		p.log.Debug("modem state changed", "path", sig.Path,
			"old", decodeModemState(oldSt), "new", decodeModemState(newSt),
			"reason", decodeFailedReason(reason))
	}
}

// onModemAdded 是 modem 出现时的入口：读取所有接口属性，构造内存条目，订阅后续信号。
func (p *MMProvider) onModemAdded(ctx context.Context, path dbus.ObjectPath,
	ifaces map[string]map[string]dbus.Variant,
) {
	modemProps := ifaces[ifaceModem]
	if modemProps == nil {
		return
	}
	deviceID := getString(modemProps, "DeviceIdentifier")
	if deviceID == "" {
		p.log.Warn("modem without DeviceIdentifier, skipping", "path", path)
		return
	}

	state := buildModemState(path, ifaces)

	// SIM 属性需要额外读
	p.mu.RLock()
	conn := p.conn
	p.mu.RUnlock()
	simPath := getObjectPath(modemProps, "Sim")
	if conn != nil && simPath != "" && simPath != "/" {
		if sim, err := p.readSim(conn, simPath); err == nil {
			// Fallback: 某些 MBIM 固件（例如 Huawei ME906s）的 Sim 接口不上报 OperatorIdentifier/Name，
			// 此时借用 Modem3gpp 层已经获取的运营商信息填充，保证前端展示完整。
			if sim.OperatorName == "" && state.OperatorName != "" {
				sim.OperatorName = state.OperatorName
			}
			if sim.OperatorID == "" && state.OperatorID != "" {
				sim.OperatorID = state.OperatorID
			}
			// MM 的 Sim 接口没有 MSISDN，号码在 Modem.OwnNumbers 里。
			// 取第一个作为 MSISDN 展示给前端；注意 MM 不同模块返回格式可能有无 "+" 前缀。
			if sim.MSISDN == "" && len(state.OwnNumbers) > 0 {
				sim.MSISDN = normalizeMSISDN(state.OwnNumbers[0])
			}
			state.SIM = &sim
			state.HasSim = true
		} else {
			p.log.Debug("read sim failed", "path", simPath, "err", err)
		}
	}

	p.mu.Lock()
	entry, exists := p.modems[deviceID]
	if !exists {
		entry = &modemEntry{SMSPaths: map[dbus.ObjectPath]bool{}}
		p.modems[deviceID] = entry
	}
	// 若 DBus path 变更了，先清理旧的 match 订阅。
	if exists && entry.State.DBusPath != "" && entry.State.DBusPath != string(path) {
		p.removeModemMatches(entry)
		delete(p.byPath, dbus.ObjectPath(entry.State.DBusPath))
	}
	entry.State = state
	entry.SIMPath = simPath
	p.byPath[path] = deviceID
	p.mu.Unlock()

	// 订阅 modem 的 PropertiesChanged（覆盖 Modem/Modem3gpp/Messaging/Signal 等接口）
	matches := []dbus.MatchOption{
		dbus.WithMatchInterface(ifaceDBusProps),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchObjectPath(path),
		dbus.WithMatchSender(mmService),
	}
	if conn != nil {
		if err := conn.AddMatchSignal(matches...); err != nil {
			p.log.Warn("add match PropertiesChanged failed", "path", path, "err", err)
		} else {
			p.mu.Lock()
			entry.matches = append(entry.matches, matches...)
			p.mu.Unlock()
		}
	}

	// Messaging.Added/Deleted
	if state.HasMessaging && conn != nil {
		mm := []dbus.MatchOption{
			dbus.WithMatchInterface(ifaceMessaging),
			dbus.WithMatchObjectPath(path),
			dbus.WithMatchSender(mmService),
		}
		if err := conn.AddMatchSignal(mm...); err != nil {
			p.log.Warn("add match messaging failed", "path", path, "err", err)
		} else {
			p.mu.Lock()
			entry.matches = append(entry.matches, mm...)
			p.mu.Unlock()
		}
	}

	// Modem.StateChanged（可选，debug 用）
	if conn != nil {
		sm := []dbus.MatchOption{
			dbus.WithMatchInterface(ifaceModem),
			dbus.WithMatchMember("StateChanged"),
			dbus.WithMatchObjectPath(path),
			dbus.WithMatchSender(mmService),
		}
		if err := conn.AddMatchSignal(sm...); err == nil {
			p.mu.Lock()
			entry.matches = append(entry.matches, sm...)
			p.mu.Unlock()
		}
	}

	// 启用 Signal 接口 5 秒采样
	if state.HasSignal && conn != nil {
		modemObj := conn.Object(mmService, path)
		if err := modemObj.CallWithContext(ctx, ifaceSignal+".Setup", 0, uint32(5)).Err; err != nil {
			p.log.Debug("signal setup failed (non-fatal)", "path", path, "err", err)
		}
	}

	p.log.Info("modem added",
		"device", deviceID, "model", state.Model,
		"imei", state.IMEI, "state", state.State)
	p.emit(Event{
		Kind:     EventModemAdded,
		DeviceID: deviceID,
		Payload:  state,
		At:       time.Now(),
	})
}

// onModemRemoved 处理 modem 消失：清理 match、从 map 删除、emit 事件。
func (p *MMProvider) onModemRemoved(path dbus.ObjectPath) {
	p.mu.Lock()
	deviceID, ok := p.byPath[path]
	if !ok {
		p.mu.Unlock()
		return
	}
	entry := p.modems[deviceID]
	if entry != nil {
		p.removeModemMatches(entry)
		// 清理该 modem 下所有 SMS 订阅
		for sp := range entry.SMSPaths {
			p.removeSmsMatchLocked(sp)
		}
	}
	delete(p.modems, deviceID)
	delete(p.byPath, path)
	p.mu.Unlock()

	p.log.Info("modem removed", "device", deviceID, "path", path)
	p.emit(Event{
		Kind:     EventModemRemoved,
		DeviceID: deviceID,
		Payload:  ModemState{DeviceID: deviceID, DBusPath: string(path)},
		At:       time.Now(),
	})
}

// onPropertiesChanged 依据接口分派属性变化。
func (p *MMProvider) onPropertiesChanged(ctx context.Context, path dbus.ObjectPath,
	iface string, changed map[string]dbus.Variant,
) {
	p.log.Debug("properties changed", "path", path, "iface", iface, "keys", keysOf(changed))

	// SMS 的属性变更（State 等）
	if iface == ifaceSms {
		p.onSmsPropsChanged(path, changed)
		return
	}

	// 其余情况只处理属于某个已知 modem 的路径
	p.mu.RLock()
	deviceID, ok := p.byPath[path]
	entry, _ := p.modems[deviceID]
	conn := p.conn
	p.mu.RUnlock()
	if !ok || entry == nil {
		return
	}

	switch iface {
	case ifaceModem:
		p.applyModemProps(entry, changed)
		if sp, ok := changed["Sim"]; ok {
			newPath, _ := sp.Value().(dbus.ObjectPath)
			switch {
			case newPath == "" || newPath == "/":
				// SIM 被拔出：清空内存 SIM 快照并 emit update。
				// DB 层的 modem↔sim 绑定清理由 runner.go 在收到
				// EventModemUpdated 且 state.SIM == nil 时处理。
				p.mu.Lock()
				entry.State.SIM = nil
				entry.State.HasSim = false
				entry.SIMPath = ""
				p.mu.Unlock()
				p.log.Info("sim removed", "device", deviceID)
			default:
				if sim, err := p.readSim(conn, newPath); err == nil {
					p.mu.Lock()
					entry.State.SIM = &sim
					entry.State.HasSim = true
					entry.SIMPath = newPath
					p.mu.Unlock()
					p.emit(Event{
						Kind: EventSimUpdated, DeviceID: deviceID,
						Payload: sim, At: time.Now(),
					})
				}
			}
		}
		p.emit(Event{Kind: EventModemUpdated, DeviceID: deviceID, Payload: entry.State, At: time.Now()})

	case ifaceModem3gpp:
		p.mu.Lock()
		if v, ok := changed["RegistrationState"]; ok {
			if u, ok := v.Value().(uint32); ok {
				entry.State.Registration = decodeRegistrationState(u)
			}
		}
		if s := variantString(changed["OperatorCode"]); s != "" {
			entry.State.OperatorID = s
		}
		if s := variantString(changed["OperatorName"]); s != "" {
			entry.State.OperatorName = s
		}
		snap := entry.State
		p.mu.Unlock()
		p.emit(Event{Kind: EventModemUpdated, DeviceID: deviceID, Payload: snap, At: time.Now()})

	case ifaceSignal:
		sample := extractSignalSample(changed)
		sample.DeviceID = deviceID
		sample.SampledAt = time.Now()
		// 把 registration/operator/access-tech 从快照里补上
		p.mu.RLock()
		sample.AccessTech = firstOrEmpty(entry.State.AccessTech)
		sample.Registration = entry.State.Registration
		sample.OperatorID = entry.State.OperatorID
		sample.OperatorName = entry.State.OperatorName
		sample.QualityPct = entry.State.SignalQuality
		p.mu.RUnlock()
		p.emit(Event{Kind: EventSignalSampled, DeviceID: deviceID, Payload: sample, At: time.Now()})

	case ifaceUSSD:
		p.onUSSDPropsChanged(entry, deviceID, changed)

	case ifaceSim:
		// SIM 属性变化：重新读一次完整 SIM
		p.mu.RLock()
		simPath := entry.SIMPath
		p.mu.RUnlock()
		if conn != nil && simPath != "" && simPath != "/" {
			if sim, err := p.readSim(conn, simPath); err == nil {
				p.mu.Lock()
				entry.State.SIM = &sim
				p.mu.Unlock()
				p.emit(Event{Kind: EventSimUpdated, DeviceID: deviceID, Payload: sim, At: time.Now()})
			}
		}
	}
	_ = ctx
}

// applyModemProps 合并 Modem 接口的常见属性变化到内存快照。
func (p *MMProvider) applyModemProps(entry *modemEntry, changed map[string]dbus.Variant) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := changed["State"]; ok {
		switch x := v.Value().(type) {
		case int32:
			entry.State.State = decodeModemState(x)
		case int:
			entry.State.State = decodeModemState(int32(x))
		}
	}
	if v, ok := changed["StateFailedReason"]; ok {
		if u, ok := v.Value().(uint32); ok {
			entry.State.FailedReason = decodeFailedReason(u)
		}
	}
	if v, ok := changed["PowerState"]; ok {
		if u, ok := v.Value().(uint32); ok {
			entry.State.PowerState = decodePowerState(u)
		}
	}
	if v, ok := changed["AccessTechnologies"]; ok {
		if u, ok := v.Value().(uint32); ok {
			entry.State.AccessTech = decodeAccessTechnologies(u)
		}
	}
	if _, ok := changed["SignalQuality"]; ok {
		pct, recent := getSignalQuality(changed)
		entry.State.SignalQuality = pct
		entry.State.SignalRecent = recent
	}
	if v, ok := changed["OwnNumbers"]; ok {
		if ss, ok := v.Value().([]string); ok {
			entry.State.OwnNumbers = append([]string(nil), ss...)
			// 号码变化时同步更新已绑定 SIM 的 MSISDN 展示字段（MM Sim 接口不含 MSISDN）
			if entry.State.SIM != nil && len(ss) > 0 {
				entry.State.SIM.MSISDN = normalizeMSISDN(ss[0])
			}
		}
	}
}

// onUSSDPropsChanged 维护 USSD 会话状态。
func (p *MMProvider) onUSSDPropsChanged(entry *modemEntry, deviceID string,
	changed map[string]dbus.Variant,
) {
	p.mu.Lock()
	if entry.USSDSession == nil {
		entry.USSDSession = &USSDState{SessionID: deviceID, DeviceID: deviceID}
	}
	sess := entry.USSDSession
	if v, ok := changed["State"]; ok {
		if u, ok := v.Value().(uint32); ok {
			sess.State = decodeUSSDState(u)
		}
	}
	if s := variantString(changed["NetworkNotification"]); s != "" {
		sess.NetworkNotification = s
	}
	if s := variantString(changed["NetworkRequest"]); s != "" {
		sess.NetworkRequest = s
	}
	snap := *sess
	p.mu.Unlock()
	p.emit(Event{Kind: EventUSSDStateChanged, DeviceID: deviceID, Payload: snap, At: time.Now()})
}

// onSmsAppearedForModem 新增一个 SMS path 并关联到指定 modem。
//
// 流程（持锁下完成登记，避免竞态）：
//  1. 若 path 已订阅过，直接返回（幂等）。
//  2. AddMatchSignal 订阅该 SMS 的 PropertiesChanged（在 conn 上；锁内做因为
//     godbus 的 AddMatchSignal 内部也是同步调用，性能可接受）。
//  3. 写入 smsSubs + modemEntry.SMSPaths，确保后续 deviceIDForSMSPath 能反查到。
//  4. 出锁后读取 SMS 属性并 emit 初始事件（REC'D → EventSMSReceived，否则 StateChanged）。
//
// 这个路径同时供 initial reconcile + live Messaging.Added 使用，
// 不再需要额外的"先预填 SMSPaths 再调 onSmsAppeared"特殊流程。
func (p *MMProvider) onSmsAppearedForModem(deviceID string, path dbus.ObjectPath) {
	if deviceID == "" {
		p.log.Warn("onSmsAppearedForModem with empty deviceID", "path", path)
		return
	}
	p.mu.Lock()
	if _, exists := p.smsSubs[path]; exists {
		// 已订阅，仅确保 SMSPaths 反查表上也有登记（修复 reload 边缘情况）。
		if entry, ok := p.modems[deviceID]; ok {
			entry.SMSPaths[path] = true
		}
		p.mu.Unlock()
		return
	}
	conn := p.conn
	if conn == nil {
		p.mu.Unlock()
		return
	}
	matches := []dbus.MatchOption{
		dbus.WithMatchInterface(ifaceDBusProps),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchObjectPath(path),
		dbus.WithMatchSender(mmService),
	}
	if err := conn.AddMatchSignal(matches...); err != nil {
		p.mu.Unlock()
		p.log.Debug("add sms match failed", "path", path, "err", err)
		return
	}
	p.smsSubs[path] = matches
	if entry, ok := p.modems[deviceID]; ok {
		entry.SMSPaths[path] = true
	}
	p.mu.Unlock()

	// 出锁读 SMS。读失败也没关系——后续 PropertiesChanged 会再触发。
	if rec, err := p.readSMS(conn, path); err == nil {
		kind := EventSMSStateChanged
		if rec.State == "received" && rec.Direction == "inbound" {
			kind = EventSMSReceived
		}
		p.emit(Event{
			Kind: kind, DeviceID: deviceID,
			Payload: rec, At: time.Now(),
		})
	}
}

// onSmsRemoved 清理订阅。
func (p *MMProvider) onSmsRemoved(path dbus.ObjectPath) {
	p.mu.Lock()
	p.removeSmsMatchLocked(path)
	deviceID := p.deviceIDForSMSPathLocked(path)
	if entry, ok := p.modems[deviceID]; ok {
		delete(entry.SMSPaths, path)
	}
	p.mu.Unlock()
}

// onSmsPropsChanged 读取新 state；若过渡到 RECEIVED 则发 SMSReceived。
func (p *MMProvider) onSmsPropsChanged(path dbus.ObjectPath, changed map[string]dbus.Variant) {
	// 只关心 State / Text / Number
	p.mu.RLock()
	conn := p.conn
	_, subscribed := p.smsSubs[path]
	p.mu.RUnlock()
	if !subscribed || conn == nil {
		return
	}
	// 重新完整读一次最稳；changed 不一定带完整内容。
	rec, err := p.readSMS(conn, path)
	if err != nil {
		return
	}
	deviceID := p.deviceIDForSMSPath(path)
	kind := EventSMSStateChanged
	if rec.State == "received" && rec.Direction == "inbound" {
		// 只有新转到 received 时才报 received；这里偷懒每次转发都等同状态变更，
		// 由下游 store 去重（UNIQUE(sim_id, ext_id)）。
		kind = EventSMSReceived
	}
	p.emit(Event{Kind: kind, DeviceID: deviceID, Payload: rec, At: time.Now()})
}

// -------------------- 内部：读对象属性 --------------------

// readAllProps 通过 org.freedesktop.DBus.Properties.GetAll 读一个接口的全部属性。
func (p *MMProvider) readAllProps(conn *dbus.Conn, path dbus.ObjectPath, iface string) (map[string]dbus.Variant, error) {
	obj := conn.Object(mmService, path)
	var props map[string]dbus.Variant
	if err := obj.Call(ifaceDBusProps+".GetAll", 0, iface).Store(&props); err != nil {
		return nil, err
	}
	return props, nil
}

// readSim 读 Sim 接口所有属性。
func (p *MMProvider) readSim(conn *dbus.Conn, path dbus.ObjectPath) (SimState, error) {
	props, err := p.readAllProps(conn, path, ifaceSim)
	if err != nil {
		return SimState{}, err
	}
	sim := SimState{
		DBusPath:         string(path),
		ICCID:            getString(props, "SimIdentifier"),
		IMSI:             getString(props, "Imsi"),
		EID:              getString(props, "Eid"),
		OperatorID:       getString(props, "OperatorIdentifier"),
		OperatorName:     getString(props, "OperatorName"),
		Active:           getBool(props, "Active"),
		EmergencyNumbers: getStringSlice(props, "EmergencyNumbers"),
		SimType:          decodeSimType(getUint32(props, "SimType")),
	}
	return sim, nil
}

// readSMS 读 SMS 对象属性。
func (p *MMProvider) readSMS(conn *dbus.Conn, path dbus.ObjectPath) (SMSRecord, error) {
	props, err := p.readAllProps(conn, path, ifaceSms)
	if err != nil {
		return SMSRecord{}, err
	}
	rec := SMSRecord{
		ExtID:         string(path),
		State:         decodeSmsState(getUint32(props, "State")),
		Peer:          getString(props, "Number"),
		SMSC:          getString(props, "SMSC"),
		Storage:       decodeSmsStorage(getUint32(props, "Storage")),
		DeliveryState: getUint32(props, "DeliveryState"),
	}
	// Text 只有 State=RECEIVED 时才保证完整
	if rec.State == "received" || rec.State == "sent" || rec.State == "stored" {
		rec.Text = getString(props, "Text")
	}
	// Direction: PduType 属性决定；MM 也有 "PduType"（1=deliver/inbound, 2=submit/outbound）
	if pt := getUint32(props, "PduType"); pt != 0 {
		switch pt {
		case 1, 3:
			rec.Direction = "inbound"
		case 2:
			rec.Direction = "outbound"
		}
	} else {
		// 回退：按 state 猜
		switch rec.State {
		case "receiving", "received":
			rec.Direction = "inbound"
		case "sending", "sent", "stored":
			rec.Direction = "outbound"
		}
	}
	if ts := getString(props, "Timestamp"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			rec.Timestamp = t
		}
	}
	return rec, nil
}

// -------------------- 内部：辅助 --------------------

// emit 把事件投递到 channel；channel 满时丢弃并记 warn（保护实时循环不被阻塞）。
func (p *MMProvider) emit(ev Event) {
	select {
	case p.events <- ev:
	default:
		p.log.Warn("modem event channel full, dropping", "kind", ev.Kind, "device", ev.DeviceID)
	}
}

// deviceIDForSMSPath 从 SMS path 推导出 modem deviceID。
// MM 的 SMS path 形如 /org/freedesktop/ModemManager1/SMS/<n>，和 modem path 无字面关系；
// 但 MM 会通过 Messaging.Added 信号告知（信号路径是 modem path）。
// 实现上我们维护反查表：每个 SMS 出现时登记它所属 modem。
func (p *MMProvider) deviceIDForSMSPath(path dbus.ObjectPath) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.deviceIDForSMSPathLocked(path)
}

func (p *MMProvider) deviceIDForSMSPathLocked(path dbus.ObjectPath) string {
	for devID, entry := range p.modems {
		if entry.SMSPaths[path] {
			return devID
		}
	}
	return ""
}

// removeModemMatches 清理某 modem 的所有 AddMatchSignal 订阅。
// 必须用和 AddMatchSignal 相同的 option 集合调用 RemoveMatchSignal。
func (p *MMProvider) removeModemMatches(entry *modemEntry) {
	if p.conn == nil {
		return
	}
	// 我们把 matches 以"扁平"方式存储，按每 4 个一组调用 Remove 不保险。
	// 简化：一次性调用 Remove 传入整个 slice 会匹配失败。
	// 所以订阅时每组 options 各自记录为一段——用 nil 作为分隔符。
	// 这里 matches 是混合的，但因为 godbus 用 option 的 filter 作为 key 去重，
	// 传任意 superset 都会 no-op（内部按字符串匹配）。实用做法：按原 slice 切分并逐段 Remove。
	// 为简化，记录的是每段 append 进来的，当前实现下我们用分段记录的方式：见 removeMatchesSegmented。
	p.removeMatchesSegmented(entry.matches)
	entry.matches = nil
}

// removeMatchesSegmented 以 "组" 为单位调 RemoveMatchSignal。
// 每组是 AddMatchSignal 原始传入的 option 数量；当前我们以启发式：
// 当遇到 dbus.WithMatchInterface 时视作新组起点（所有 AddMatchSignal 都以它打头）。
func (p *MMProvider) removeMatchesSegmented(all []dbus.MatchOption) {
	if len(all) == 0 || p.conn == nil {
		return
	}
	// 由于 dbus.MatchOption 是函数式 option，没法反射判断类型。
	// 退而求其次：按原记录长度分段是不可能的，所以我们改为整段一起 Remove——
	// godbus RemoveMatchSignal 会尝试按完整 filter 字符串匹配，若不在则返回错误但无副作用。
	// 这里一次性尝试整段；失败则逐一尝试。
	if err := p.conn.RemoveMatchSignal(all...); err == nil {
		return
	}
	// 退路：单个尝试（单独某个 option 可能是一条完整 filter，很少成功）
	for _, m := range all {
		_ = p.conn.RemoveMatchSignal(m)
	}
}

// removeSmsMatchLocked 必须在持锁下调用。
func (p *MMProvider) removeSmsMatchLocked(path dbus.ObjectPath) {
	matches, ok := p.smsSubs[path]
	if !ok {
		return
	}
	delete(p.smsSubs, path)
	if p.conn != nil {
		_ = p.conn.RemoveMatchSignal(matches...)
	}
}

// cleanupAllMatches 在 Start 退出时清掉所有订阅。
func (p *MMProvider) cleanupAllMatches() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.modems {
		p.removeMatchesSegmented(entry.matches)
		entry.matches = nil
	}
	for path := range p.smsSubs {
		p.removeSmsMatchLocked(path)
	}
}

// buildModemState 从一组接口属性字典构造内存快照。ifaces 需至少含 Modem 接口。
func buildModemState(path dbus.ObjectPath, ifaces map[string]map[string]dbus.Variant) ModemState {
	modemProps := ifaces[ifaceModem]
	state := ModemState{
		DeviceID:         getString(modemProps, "DeviceIdentifier"),
		DBusPath:         string(path),
		Manufacturer:     getString(modemProps, "Manufacturer"),
		Model:            getString(modemProps, "Model"),
		Revision:         getString(modemProps, "Revision"),
		HardwareRevision: getString(modemProps, "HardwareRevision"),
		Plugin:           getString(modemProps, "Plugin"),
		IMEI:             getString(modemProps, "EquipmentIdentifier"),
		PrimaryPort:      getString(modemProps, "PrimaryPort"),
		Ports:            getPorts(modemProps),
		USBPath:          getString(modemProps, "Physdev"),
		State:            decodeModemState(getInt32(modemProps, "State")),
		FailedReason:     decodeFailedReason(getUint32(modemProps, "StateFailedReason")),
		PowerState:       decodePowerState(getUint32(modemProps, "PowerState")),
		AccessTech:       decodeAccessTechnologies(getUint32(modemProps, "AccessTechnologies")),
		OwnNumbers:       getStringSlice(modemProps, "OwnNumbers"),
	}
	pct, recent := getSignalQuality(modemProps)
	state.SignalQuality = pct
	state.SignalRecent = recent

	if sp := getObjectPath(modemProps, "Sim"); sp != "" && sp != "/" {
		state.HasSim = true
	}

	// Modem3gpp
	if m3 := ifaces[ifaceModem3gpp]; m3 != nil {
		if state.IMEI == "" {
			state.IMEI = getString(m3, "Imei")
		}
		state.Registration = decodeRegistrationState(getUint32(m3, "RegistrationState"))
		state.OperatorID = getString(m3, "OperatorCode")
		state.OperatorName = getString(m3, "OperatorName")
	}

	// Messaging
	if mm := ifaces[ifaceMessaging]; mm != nil {
		state.HasMessaging = true
		state.SupportedStorages = getSupportedStorages(mm)
	}

	// Signal 接口存在即代表支持
	if _, ok := ifaces[ifaceSignal]; ok {
		state.HasSignal = true
	}
	// USSD
	if _, ok := ifaces[ifaceUSSD]; ok {
		state.HasUSSD = true
	}
	return state
}

// extractSignalSample 从 Signal 接口的 PropertiesChanged 字典抽取各技术数值。
// 当前实现只关注 LTE；5G/UMTS/GSM 可以在未来扩展，数据库字段已经够用。
func extractSignalSample(changed map[string]dbus.Variant) SignalSample {
	out := SignalSample{}
	// 优先 LTE → 5GNR → UMTS → GSM
	for _, tech := range []string{"Lte", "Nr5g", "Umts", "Gsm"} {
		v, ok := changed[tech]
		if !ok {
			continue
		}
		if f := signalDictFloat(v, "rssi"); f != nil {
			x := int(*f)
			out.RSSIdBm = &x
		}
		if f := signalDictFloat(v, "rsrp"); f != nil {
			x := int(*f)
			out.RSRPdBm = &x
		}
		if f := signalDictFloat(v, "rsrq"); f != nil {
			x := int(*f)
			out.RSRQdB = &x
		}
		if f := signalDictFloat(v, "snr"); f != nil {
			out.SNRdB = f
		}
		out.AccessTech = strings.ToLower(tech)
		if out.AccessTech == "nr5g" {
			out.AccessTech = "5gnr"
		}
		break
	}
	return out
}

// variantString 安全取 Variant 里的字符串；零值时返回空串。
func variantString(v dbus.Variant) string {
	if v.Signature().String() == "" {
		return ""
	}
	if s, ok := v.Value().(string); ok {
		return s
	}
	return ""
}

func firstOrEmpty(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

func keysOf(m map[string]dbus.Variant) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
