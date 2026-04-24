package modem

import (
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

// TestDecodeModemState 覆盖关键枚举值（特别是 FAILED=-1 的 int32 分支）。
func TestDecodeModemState(t *testing.T) {
	cases := map[int32]ModemStateEnum{
		-1: ModemStateFailed,
		0:  ModemStateUnknown,
		1:  ModemStateInitializing,
		8:  ModemStateRegistered,
		11: ModemStateConnected,
		99: ModemStateUnknown,
	}
	for in, want := range cases {
		if got := decodeModemState(in); got != want {
			t.Errorf("decodeModemState(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestDecodeAccessTechnologies 确认 bitmask 解析保序。
func TestDecodeAccessTechnologies(t *testing.T) {
	got := decodeAccessTechnologies(0x4000 | 0x0020) // lte + umts
	want := []string{"lte", "umts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if decodeAccessTechnologies(0) != nil {
		t.Error("expected nil for zero mask")
	}
}

// TestGetSignalQuality 覆盖 (ub) struct 解包。
func TestGetSignalQuality(t *testing.T) {
	props := map[string]dbus.Variant{
		// DBus STRUCT 反序列化后是 []interface{}
		"SignalQuality": dbus.MakeVariantWithSignature(
			[]interface{}{uint32(73), true}, dbus.SignatureOf(struct {
				U uint32
				B bool
			}{}),
		),
	}
	pct, recent := getSignalQuality(props)
	if pct != 73 || !recent {
		t.Errorf("got (%d,%v), want (73,true)", pct, recent)
	}

	// 缺失 key
	pct2, recent2 := getSignalQuality(map[string]dbus.Variant{})
	if pct2 != 0 || recent2 {
		t.Errorf("missing key should yield zero, got (%d,%v)", pct2, recent2)
	}
}

// TestGetPorts 覆盖 a(su) 解包。
func TestGetPorts(t *testing.T) {
	raw := [][]interface{}{
		{"ttyUSB2", uint32(3)}, // at
		{"wwan0", uint32(2)},   // net
		{"cdc-wdm0", uint32(6)}, // qmi
	}
	props := map[string]dbus.Variant{
		"Ports": dbus.MakeVariant(raw),
	}
	ports := getPorts(props)
	if len(ports) != 3 {
		t.Fatalf("len=%d want 3", len(ports))
	}
	if ports[0].Name != "ttyUSB2" || ports[0].Type != "at" {
		t.Errorf("port0 = %+v", ports[0])
	}
	if ports[2].Type != "qmi" {
		t.Errorf("port2 type = %q", ports[2].Type)
	}
}

// TestGetInt32HandlesFailedState 如果把 State 读成 uint32，FAILED(-1) 会变成 4294967295，是常见 bug。
func TestGetInt32HandlesFailedState(t *testing.T) {
	props := map[string]dbus.Variant{
		"State": dbus.MakeVariant(int32(-1)),
	}
	if got := getInt32(props, "State"); got != -1 {
		t.Errorf("got %d, want -1", got)
	}
	if decodeModemState(getInt32(props, "State")) != ModemStateFailed {
		t.Error("expected failed state")
	}
}

// TestDecodeSmsState 覆盖 RECEIVED=3 这条关键路径。
func TestDecodeSmsState(t *testing.T) {
	if decodeSmsState(3) != "received" {
		t.Fatal("state 3 should be received")
	}
	if decodeSmsState(5) != "sent" {
		t.Fatal("state 5 should be sent")
	}
}

// TestDecodePortType 覆盖常见端口类型。
func TestDecodePortType(t *testing.T) {
	cases := map[uint32]string{
		2: "net", 3: "at", 6: "qmi", 7: "mbim", 9: "ignored", 99: "unknown",
	}
	for in, want := range cases {
		if got := decodePortType(in); got != want {
			t.Errorf("decodePortType(%d)=%q want %q", in, got, want)
		}
	}
}

// TestGetStringSliceCopy 确保不共享底层数组。
func TestGetStringSliceCopy(t *testing.T) {
	src := []string{"+111", "+222"}
	props := map[string]dbus.Variant{
		"OwnNumbers": dbus.MakeVariant(src),
	}
	out := getStringSlice(props, "OwnNumbers")
	out[0] = "MUTATED"
	if src[0] == "MUTATED" {
		t.Error("getStringSlice returned shared slice")
	}
}
