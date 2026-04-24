// message.go —— WS 消息封装，以及 modem.Event → WS message 的转换。
package ws

import (
	"time"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// Message 是 server ↔ client 消息的通用信封。
//
// 字段顺序与前端 WsMessage 对齐（data 而非 payload 更通用；前端可轻改）。
type Message struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
	TS   string `json:"ts"`
}

// toWSMessage 把 runner 事件转为 WS 推送消息。
// 若某类事件不希望推送（例如纯内部），返回 ok=false。
func toWSMessage(ev modem.Event) (Message, bool) {
	ts := ev.At
	if ts.IsZero() {
		ts = time.Now()
	}
	out := Message{TS: ts.UTC().Format(time.RFC3339Nano)}
	switch ev.Kind {
	case modem.EventModemAdded:
		out.Type = "modem.added"
		out.Data = ev.Payload
	case modem.EventModemUpdated:
		out.Type = "modem.updated"
		out.Data = ev.Payload
	case modem.EventModemRemoved:
		out.Type = "modem.removed"
		out.Data = map[string]any{"device_id": ev.DeviceID}
	case modem.EventSimUpdated:
		out.Type = "sim.updated"
		out.Data = map[string]any{
			"device_id": ev.DeviceID,
			"sim":       ev.Payload,
		}
	case modem.EventSignalSampled:
		out.Type = "signal.sample"
		out.Data = ev.Payload
	case modem.EventSMSReceived:
		out.Type = "sms.received"
		out.Data = map[string]any{
			"device_id": ev.DeviceID,
			"sms":       ev.Payload,
		}
	case modem.EventSMSStateChanged:
		out.Type = "sms.state_changed"
		out.Data = map[string]any{
			"device_id": ev.DeviceID,
			"sms":       ev.Payload,
		}
	case modem.EventUSSDStateChanged:
		out.Type = "ussd.state"
		out.Data = map[string]any{
			"device_id": ev.DeviceID,
			"ussd":      ev.Payload,
		}
	default:
		return Message{}, false
	}
	return out, true
}
