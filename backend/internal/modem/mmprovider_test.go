package modem

import (
	"testing"
	"time"
)

func TestParseMMTimestampOffsetHourOnly(t *testing.T) {
	got, err := parseMMTimestamp("2026-04-23T16:20:35+02")
	if err != nil {
		t.Fatalf("parseMMTimestamp: %v", err)
	}
	want := time.Date(2026, 4, 23, 14, 20, 35, 0, time.UTC)
	if !got.UTC().Equal(want) {
		t.Fatalf("got %s want %s", got.UTC().Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestClassifySMSObservation(t *testing.T) {
	receivedInbound := SMSRecord{Direction: "inbound", State: "received", Text: "hi"}

	tests := []struct {
		name         string
		prev         smsObserved
		appearedLive bool
		rec          SMSRecord
		want         EventKind
	}{
		{
			name: "initial received historical only state changed",
			rec:  receivedInbound,
			want: EventSMSStateChanged,
		},
		{
			name:         "live first read already received notifies",
			appearedLive: true,
			rec:          receivedInbound,
			want:         EventSMSReceived,
		},
		{
			name: "live first read failed then props received notifies",
			prev: smsObserved{LiveAdded: true},
			rec:  receivedInbound,
			want: EventSMSReceived,
		},
		{
			name: "inbound receiving to received notifies",
			prev: smsObserved{Direction: "inbound", State: "receiving"},
			rec:  receivedInbound,
			want: EventSMSReceived,
		},
		{
			name: "already notified suppresses",
			prev: smsObserved{LiveAdded: true, Notified: true, Direction: "inbound", State: "received"},
			rec:  receivedInbound,
			want: EventSMSStateChanged,
		},
		{
			name: "outbound never sms received",
			rec:  SMSRecord{Direction: "outbound", State: "received", Text: "hi"},
			want: EventSMSStateChanged,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySMSObservation(tt.prev, tt.rec, tt.appearedLive); got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}

func TestIsHistoricalReceivedSMS(t *testing.T) {
	now := time.Date(2026, 4, 26, 5, 0, 0, 0, time.UTC)
	if !isHistoricalReceivedSMS(SMSRecord{Timestamp: now.Add(-time.Hour)}, now) {
		t.Fatal("one hour old SMSC timestamp should be historical")
	}
	if isHistoricalReceivedSMS(SMSRecord{Timestamp: now.Add(-30 * time.Second)}, now) {
		t.Fatal("recent SMSC timestamp should be treated as live")
	}
	if isHistoricalReceivedSMS(SMSRecord{}, now) {
		t.Fatal("missing timestamp should not be treated as historical by provider")
	}
}

func TestObserveSMSInitialReceivedMarksHandled(t *testing.T) {
	observed := observeSMS(smsObserved{}, SMSRecord{Direction: "inbound", State: "received"}, EventSMSStateChanged)
	if !observed.Notified {
		t.Fatal("initial historical received sms should be marked handled/suppressed")
	}
	if got := classifySMSObservation(observed, SMSRecord{Direction: "inbound", State: "received"}, true); got != EventSMSStateChanged {
		t.Fatalf("live duplicate after initial baseline should stay state_changed, got %s", got)
	}
}
