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
	r.handle(ctx, Event{Kind: EventModemAdded, DeviceID: stateWithSIM.DeviceID, Payload: stateWithSIM, At: time.Now()})

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
	r.handle(ctx, Event{Kind: EventModemUpdated, DeviceID: stateNoSIM.DeviceID, Payload: stateNoSIM, At: time.Now()})

	row, err = store.GetModemByDeviceID(ctx, stateWithSIM.DeviceID)
	if err != nil {
		t.Fatalf("get modem after remove: %v", err)
	}
	if row.SIM != nil {
		t.Fatalf("expected sim binding removed, got %+v", row.SIM)
	}
}
