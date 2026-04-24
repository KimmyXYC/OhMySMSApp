package telegram

import (
	"strings"
	"testing"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

func TestEscapeMarkdownV2(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"hello world", "hello world"},
		{"a.b", `a\.b`},
		{"price: $5.99 (usd)", `price: $5\.99 \(usd\)`},
		{"*bold*", `\*bold\*`},
		{"[link](x)", `\[link\]\(x\)`},
		{"a_b", `a\_b`},
		{"100%!", `100%\!`},
		{"back\\slash", `back\\slash`},
		{"`code`", "\\`code\\`"},
		{"中文 没有特殊字符", "中文 没有特殊字符"},
	}
	for _, c := range cases {
		got := escapeMarkdownV2(c.in)
		if got != c.want {
			t.Errorf("escape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeMarkdownV2_AllSpecials(t *testing.T) {
	// 每个 special 都应被前置反斜杠
	specials := "_*[]()~`>#+-=|{}.!\\"
	for _, r := range specials {
		s := string(r)
		got := escapeMarkdownV2(s)
		if got != "\\"+s {
			t.Errorf("escape(%q) = %q, want %q", s, got, "\\"+s)
		}
	}
}

func TestFormatSMSNotification(t *testing.T) {
	rec := modem.SMSRecord{
		Peer: "+86 1234",
		Text: "Your code: 654321.",
	}
	out := formatSMSNotification(rec, "EC20F", "giffgaff", "dev1", 2)
	// Sanity checks：关键词出现
	for _, key := range []string{"📩", "新短信", "EC20F", "giffgaff", "654321"} {
		if !strings.Contains(out, key) {
			t.Errorf("output missing %q:\n%s", key, out)
		}
	}
	// index 应被转义
	if !strings.Contains(out, `\[\#2\]`) {
		t.Errorf("expected escaped [#2]: %s", out)
	}
}

func TestFormatModemOverview_Empty(t *testing.T) {
	out := formatModemOverview(nil)
	if !strings.Contains(out, "没有") {
		t.Errorf("want empty msg, got: %s", out)
	}
}

func TestFormatModemOverview_NonEmpty(t *testing.T) {
	modems := []modem.ModemState{
		{
			DeviceID:      "dev-1",
			Model:         "EC20F",
			IMEI:          "123456789012345",
			State:         modem.ModemStateConnected,
			OperatorName:  "giffgaff",
			OperatorID:    "23410",
			SignalQuality: 80,
			SignalRecent:  true,
		},
	}
	out := formatModemOverview(modems)
	for _, key := range []string{"EC20F", "connected", "giffgaff", "80"} {
		if !strings.Contains(out, key) {
			t.Errorf("expected %q in:\n%s", key, out)
		}
	}
}

func TestFormatSignalOverview(t *testing.T) {
	modems := []modem.ModemState{
		{DeviceID: "d1", Model: "EC20F", SignalQuality: 70, SignalRecent: true, AccessTech: []string{"lte"}},
		{DeviceID: "d2", Model: "ME906s", SignalRecent: false},
	}
	out := formatSignalOverview(modems)
	if !strings.Contains(out, "EC20F") || !strings.Contains(out, "ME906s") {
		t.Errorf("names missing: %s", out)
	}
	if !strings.Contains(out, "70") {
		t.Errorf("expected 70: %s", out)
	}
	if !strings.Contains(out, "n/a") {
		t.Errorf("expected n/a for offline: %s", out)
	}
}

func TestFormatRecentSMS(t *testing.T) {
	rows := []modem.SMSRow{
		{ID: 1, Direction: "inbound", Peer: "+111", Body: "hello", TsCreated: "2025-01-01T10:00:00Z"},
		{ID: 2, Direction: "outbound", Peer: "+222", Body: "world", TsCreated: "2025-01-01T10:01:00Z"},
	}
	out := formatRecentSMS(rows)
	for _, key := range []string{"最近短信", "+111", "+222", "hello", "world"} {
		if !strings.Contains(out, key) {
			t.Errorf("expected %q in:\n%s", key, out)
		}
	}
}

func TestTruncate(t *testing.T) {
	if truncate("abc", 10) != "abc" {
		t.Fail()
	}
	if truncate("abcdefgh", 3) != "abc..." {
		t.Fail()
	}
}

func TestShortTime(t *testing.T) {
	if shortTime("") != "-" {
		t.Fail()
	}
	if shortTime("garbage") != "garbage" {
		t.Fail()
	}
	// 真实 RFC3339 应被缩短
	got := shortTime("2025-04-24T10:30:00Z")
	if len(got) != len("01-02 15:04") {
		t.Errorf("short form len wrong: %q", got)
	}
}
