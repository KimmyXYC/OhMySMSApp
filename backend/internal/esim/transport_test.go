package esim

import (
	"testing"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

func sp(s string) *string { return &s }

func TestResolveTransport_PreferQMI(t *testing.T) {
	m := &modem.ModemRow{QMIPort: sp("cdc-wdm3"), MBIMPort: sp("cdc-wdm5")}
	got, ok := resolveTransport(m)
	if !ok {
		t.Fatal("expected ok")
	}
	if got.Kind != "qmi" || got.Device != "/dev/cdc-wdm3" {
		t.Errorf("got %+v, want qmi /dev/cdc-wdm3", got)
	}
}

func TestResolveTransport_FallbackMBIM(t *testing.T) {
	m := &modem.ModemRow{MBIMPort: sp("cdc-wdm1")}
	got, ok := resolveTransport(m)
	if !ok || got.Kind != "mbim" {
		t.Fatalf("expected mbim, got %+v ok=%v", got, ok)
	}
}

func TestResolveTransport_None(t *testing.T) {
	m := &modem.ModemRow{}
	if _, ok := resolveTransport(m); ok {
		t.Error("expected ok=false when no qmi/mbim")
	}
}

func TestResolveTransport_AbsolutePathPreserved(t *testing.T) {
	m := &modem.ModemRow{QMIPort: sp("/dev/cdc-wdm0")}
	got, ok := resolveTransport(m)
	if !ok || got.Device != "/dev/cdc-wdm0" {
		t.Errorf("got %+v", got)
	}
}

func TestResolveTransport_NilSafe(t *testing.T) {
	if _, ok := resolveTransport(nil); ok {
		t.Error("expected ok=false on nil")
	}
}
