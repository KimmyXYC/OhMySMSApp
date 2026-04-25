package esim

import "testing"

func TestVendorFromEID(t *testing.T) {
	cases := []struct {
		eid  string
		want string
	}{
		{"89086030202200000024000011265376", "5ber"},
		{"35840574202500000125000004509296", "9esim"},
		{"89044045123456789012345678901234", "9esim"},
		{"00000000000000000000000000000000", "unknown"},
		{"", "unknown"},
		{"  89086030abcd  ", "5ber"}, // 前后空白
	}
	for _, c := range cases {
		got := vendorFromEID(c.eid)
		if got != c.want {
			t.Errorf("vendorFromEID(%q) = %q, want %q", c.eid, got, c.want)
		}
	}
}

func TestVendorDisplay(t *testing.T) {
	if vendorDisplay("5ber") != "5ber.eSIM" {
		t.Error("5ber display")
	}
	if vendorDisplay("9esim") != "9eSIM" {
		t.Error("9esim display")
	}
	if vendorDisplay("anything") != "Unknown eUICC" {
		t.Error("unknown display")
	}
}
