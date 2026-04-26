package modem

import (
	"context"
	"testing"
	"time"
)

func TestRunnerModemUpdatedWithoutSIMUnbinds(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	r := NewRunner(nil, store, nil)

	stateWithSIM := ModemState{
		DeviceID: "dev-sim-remove",
		IMEI:     "111122223333555",
		SIM:      &SimState{ICCID: "8986000000000000001", IMSI: "460010000000001"},
		HasSim:   true,
	}
	evAdd := Event{Kind: EventModemAdded, DeviceID: stateWithSIM.DeviceID, Payload: stateWithSIM, At: time.Now()}
	r.handle(ctx, &evAdd)

	row, err := store.GetModemByDeviceID(ctx, stateWithSIM.DeviceID)
	if err != nil {
		t.Fatalf("get modem: %v", err)
	}
	if row.SIM == nil || row.SIM.ICCID != "8986000000000000001" {
		t.Fatalf("expected bound sim, got %+v", row.SIM)
	}

	stateNoSIM := stateWithSIM
	stateNoSIM.SIM = nil
	stateNoSIM.HasSim = false
	evUpdate := Event{Kind: EventModemUpdated, DeviceID: stateNoSIM.DeviceID, Payload: stateNoSIM, At: time.Now()}
	r.handle(ctx, &evUpdate)

	row, err = store.GetModemByDeviceID(ctx, stateWithSIM.DeviceID)
	if err != nil {
		t.Fatalf("get modem after remove: %v", err)
	}
	if row.SIM != nil {
		t.Fatalf("expected sim binding removed, got %+v", row.SIM)
	}
}

func TestRunnerSMSReceivedDedupSuppressesFanout(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	r := NewRunner(nil, store, nil)
	modemID, err := store.UpsertModem(ctx, ModemState{DeviceID: "dev-sms-dedup", IMEI: "111122223333556"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertSim(ctx, SimState{ICCID: "8986000000000000999"}, modemID); err != nil {
		t.Fatal(err)
	}
	r.setModemID("dev-sms-dedup", modemID)

	ev := Event{
		Kind:     EventSMSReceived,
		DeviceID: "dev-sms-dedup",
		Payload: SMSRecord{
			ExtID:     "/org/freedesktop/ModemManager1/SMS/1",
			Direction: "inbound",
			State:     "received",
			Peer:      "+10086",
			Text:      "hello",
		},
		At: time.Now(),
	}
	if !r.handle(ctx, &ev) {
		t.Fatal("first sms should fanout")
	}
	if r.handle(ctx, &ev) {
		t.Fatal("duplicate sms should be suppressed")
	}
}
