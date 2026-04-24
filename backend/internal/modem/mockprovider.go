package modem

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MockProvider 用于开发机本地跑（无 DBus）。产生两张假 modem + 周期性事件。
//
// 行为：
//   - 启动时 emit 两条 ModemAdded（pSIM + sticker eSIM 场景）
//   - 每 5s 给每张 modem emit 一次 SignalSampled（随机波动）
//   - SendSMS 本地回显为 outbound SMS，ext_id 自增
//   - USSD 命令 "*101#" 返回固定余额文本；其余命令回假错误文字
type MockProvider struct {
	log    *slog.Logger
	events chan Event

	mu       sync.RWMutex
	modems   map[string]*ModemState
	smsByID  map[string][]SMSRecord // deviceID → sms list
	smsSeq   atomic.Int64
	ussdSeq  atomic.Int64
	running  atomic.Bool
}

// NewMockProvider 构造 MockProvider。
func NewMockProvider(log *slog.Logger) *MockProvider {
	if log == nil {
		log = slog.Default()
	}
	m := &MockProvider{
		log:     log,
		events:  make(chan Event, 128),
		modems:  make(map[string]*ModemState),
		smsByID: make(map[string][]SMSRecord),
	}

	// 假 modem 1：Huawei ME906s，pSIM
	m1 := ModemState{
		DeviceID: "mock-device-quectel-0001", DBusPath: "/org/freedesktop/ModemManager1/Modem/0",
		Manufacturer: "QUECTEL", Model: "EC20-CE", Revision: "EC20CEFAR02A19M4G",
		Plugin: "quectel", IMEI: "860000000000001", PrimaryPort: "cdc-wdm0",
		Ports: []Port{
			{Name: "wwan0", Type: "net"}, {Name: "cdc-wdm0", Type: "qmi"},
			{Name: "ttyUSB2", Type: "at"}, {Name: "ttyUSB3", Type: "at"},
		},
		USBPath:    "/sys/devices/platform/xhci-hcd.0.auto/usb1/1-1",
		State:      ModemStateRegistered, PowerState: "on",
		AccessTech: []string{"lte"}, SignalQuality: 75, SignalRecent: true,
		Registration: "home", OperatorID: "46001", OperatorName: "China Unicom",
		OwnNumbers:   []string{"+8613800138000"},
		HasSim:       true,
		SIM: &SimState{
			DBusPath:   "/org/freedesktop/ModemManager1/SIM/0",
			ICCID:      "89860000000000001234",
			IMSI:       "460010000000001",
			MSISDN:     "+8613800138000",
			OperatorID: "46001", OperatorName: "China Unicom",
			Active: true, SimType: "physical",
		},
		HasUSSD: true, HasSignal: true, HasMessaging: true,
		SupportedStorages: []string{"sm", "me"},
	}
	// 假 modem 2：sticker eSIM
	m2 := ModemState{
		DeviceID: "mock-device-huawei-0002", DBusPath: "/org/freedesktop/ModemManager1/Modem/1",
		Manufacturer: "HUAWEI", Model: "ME906s", Revision: "11.839.01.00.00",
		Plugin: "huawei", IMEI: "860000000000002", PrimaryPort: "cdc-wdm1",
		Ports: []Port{
			{Name: "wwan1", Type: "net"}, {Name: "cdc-wdm1", Type: "mbim"},
		},
		USBPath:    "/sys/devices/platform/xhci-hcd.0.auto/usb1/1-2",
		State:      ModemStateRegistered, PowerState: "on",
		AccessTech: []string{"lte"}, SignalQuality: 60, SignalRecent: true,
		Registration: "roaming", OperatorID: "26201", OperatorName: "Telekom.de",
		HasSim:       true,
		SIM: &SimState{
			DBusPath:   "/org/freedesktop/ModemManager1/SIM/1",
			ICCID:      "89490200001234567890",
			IMSI:       "262010000000002",
			EID:        "89049032123412341234123412345678",
			MSISDN:     "+491771234567",
			OperatorID: "26201", OperatorName: "Telekom.de",
			Active: true, SimType: "esim",
		},
		HasUSSD: false, HasSignal: true, HasMessaging: true,
		SupportedStorages: []string{"me"},
	}
	m.modems[m1.DeviceID] = &m1
	m.modems[m2.DeviceID] = &m2
	return m
}

// Events 实现 Provider。
func (p *MockProvider) Events() <-chan Event { return p.events }

// Start 发初始事件，然后按定时器产生信号事件，直到 ctx 取消。
func (p *MockProvider) Start(ctx context.Context) error {
	p.running.Store(true)
	defer p.running.Store(false)

	// 首次 emit 所有 modem + sim
	for _, m := range p.modems {
		state := *m
		p.safeEmit(Event{Kind: EventModemAdded, DeviceID: m.DeviceID, Payload: state, At: time.Now()})
		if m.SIM != nil {
			p.safeEmit(Event{Kind: EventSimUpdated, DeviceID: m.DeviceID, Payload: *m.SIM, At: time.Now()})
		}
	}
	p.log.Info("modem mock provider started", "modems", len(p.modems))

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.mu.Lock()
			for id, m := range p.modems {
				// 小幅抖动
				m.SignalQuality = clamp(m.SignalQuality+rng.Intn(11)-5, 15, 95)
				sample := SignalSample{
					DeviceID: id, QualityPct: m.SignalQuality,
					AccessTech:   firstOrEmpty(m.AccessTech),
					Registration: m.Registration,
					OperatorID:   m.OperatorID, OperatorName: m.OperatorName,
					SampledAt: time.Now(),
				}
				rssi := -60 - rng.Intn(40)
				rsrp := -85 - rng.Intn(30)
				rsrq := -8 - rng.Intn(12)
				sample.RSSIdBm = &rssi
				sample.RSRPdBm = &rsrp
				sample.RSRQdB = &rsrq
				p.safeEmit(Event{Kind: EventSignalSampled, DeviceID: id, Payload: sample, At: time.Now()})
			}
			p.mu.Unlock()
		}
	}
}

// ListModems 实现 Provider。
func (p *MockProvider) ListModems() []ModemState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ModemState, 0, len(p.modems))
	for _, m := range p.modems {
		out = append(out, *m)
	}
	return out
}

// GetModem 实现 Provider。
func (p *MockProvider) GetModem(deviceID string) (ModemState, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	m, ok := p.modems[deviceID]
	if !ok {
		return ModemState{}, false
	}
	return *m, true
}

// ListSMS 实现 Provider。
func (p *MockProvider) ListSMS(deviceID string) ([]SMSRecord, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.modems[deviceID]; !ok {
		return nil, fmt.Errorf("modem %s not found", deviceID)
	}
	src := p.smsByID[deviceID]
	out := make([]SMSRecord, len(src))
	copy(out, src)
	return out, nil
}

// SendSMS 回显为一条 outbound sent SMS。
func (p *MockProvider) SendSMS(_ context.Context, deviceID, to, text string) (string, error) {
	p.mu.Lock()
	if _, ok := p.modems[deviceID]; !ok {
		p.mu.Unlock()
		return "", fmt.Errorf("modem %s not found", deviceID)
	}
	id := p.smsSeq.Add(1)
	extID := fmt.Sprintf("/mock/sms/%d", id)
	rec := SMSRecord{
		ExtID: extID, Direction: "outbound", State: "sent",
		Peer: to, Text: text, Timestamp: time.Now(), Storage: "me",
	}
	p.smsByID[deviceID] = append(p.smsByID[deviceID], rec)
	p.mu.Unlock()
	p.safeEmit(Event{Kind: EventSMSStateChanged, DeviceID: deviceID, Payload: rec, At: time.Now()})
	return extID, nil
}

// DeleteSMS 从内存列表移除。
func (p *MockProvider) DeleteSMS(_ context.Context, deviceID, extID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.smsByID[deviceID]
	kept := list[:0]
	for _, r := range list {
		if r.ExtID != extID {
			kept = append(kept, r)
		}
	}
	p.smsByID[deviceID] = kept
	return nil
}

// InitiateUSSD 简易响应：*101# 返回余额；其他返回错误字符串。
func (p *MockProvider) InitiateUSSD(_ context.Context, deviceID, command string) (string, string, error) {
	p.mu.RLock()
	_, ok := p.modems[deviceID]
	p.mu.RUnlock()
	if !ok {
		return "", "", fmt.Errorf("modem %s not found", deviceID)
	}
	sid := fmt.Sprintf("%s-ussd-%d", deviceID, p.ussdSeq.Add(1))
	var reply string
	cmd := strings.TrimSpace(command)
	switch cmd {
	case "*101#":
		reply = "Your balance is $12.34 (mock)"
	case "*100#":
		reply = "MSISDN: +8613800138000 (mock)"
	default:
		reply = "Unknown USSD command (mock)"
	}
	p.safeEmit(Event{
		Kind: EventUSSDStateChanged, DeviceID: deviceID,
		Payload: USSDState{
			SessionID: sid, DeviceID: deviceID, State: "idle",
			LastRequest: command, LastResponse: reply,
		},
		At: time.Now(),
	})
	return sid, reply, nil
}

// RespondUSSD mock 不维持多轮，固定回 "session closed"。
func (p *MockProvider) RespondUSSD(_ context.Context, _, _ string) (string, error) {
	return "mock session already closed", nil
}

// CancelUSSD 总是成功。
func (p *MockProvider) CancelUSSD(_ context.Context, _ string) error { return nil }

// safeEmit 非阻塞投递（channel 满时丢弃）。
func (p *MockProvider) safeEmit(ev Event) {
	select {
	case p.events <- ev:
	default:
		p.log.Warn("mock event channel full, dropping", "kind", ev.Kind)
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
