package modem

import (
	"context"
	"testing"
	"time"
)

type runnerTestProvider struct {
	events chan Event
}

func newRunnerTestProvider() *runnerTestProvider {
	return &runnerTestProvider{events: make(chan Event, 16)}
}

func (p *runnerTestProvider) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (p *runnerTestProvider) Events() <-chan Event { return p.events }

func (p *runnerTestProvider) ListModems() []ModemState { return nil }

func (p *runnerTestProvider) GetModem(string) (ModemState, bool) { return ModemState{}, false }

func (p *runnerTestProvider) ListSMS(string) ([]SMSRecord, error) { return nil, nil }

func (p *runnerTestProvider) SendSMS(context.Context, string, string, string) (string, error) {
	return "", nil
}

func (p *runnerTestProvider) DeleteSMS(context.Context, string, string) error { return nil }

func (p *runnerTestProvider) InitiateUSSD(context.Context, string, string) (string, string, error) {
	return "", "", nil
}

func (p *runnerTestProvider) RespondUSSD(context.Context, string, string) (string, error) {
	return "", nil
}

func (p *runnerTestProvider) CancelUSSD(context.Context, string) error { return nil }

func (p *runnerTestProvider) ResetModem(context.Context, string) error { return nil }

func readRunnerEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for runner event")
		return Event{}
	}
}

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
	r.handle(ctx, evAdd)

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
	r.handle(ctx, evUpdate)

	row, err = store.GetModemByDeviceID(ctx, stateWithSIM.DeviceID)
	if err != nil {
		t.Fatalf("get modem after remove: %v", err)
	}
	if row.SIM != nil {
		t.Fatalf("expected sim binding removed, got %+v", row.SIM)
	}
}

func TestRunnerDuplicateSMSReceivedDoesNotFanout(t *testing.T) {
	store := newTestStore(t)
	prov := newRunnerTestProvider()
	r := NewRunner(prov, store, nil)
	ch, unsub := r.Subscribe(8)
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("runner returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Errorf("runner did not stop")
		}
	})

	rec := SMSRecord{ExtID: "/mm/sms/1", Direction: "inbound", State: "received", Peer: "+1", Text: "hello"}
	prov.events <- Event{Kind: EventSMSReceived, DeviceID: "dev-runner", Payload: rec, At: time.Now()}
	if ev := readRunnerEvent(t, ch); ev.Kind != EventSMSReceived {
		t.Fatalf("first sms should fanout, got %s", ev.Kind)
	}

	prov.events <- Event{Kind: EventSMSReceived, DeviceID: "dev-runner", Payload: rec, At: time.Now()}
	marker := Event{Kind: EventModemRemoved, DeviceID: "marker", Payload: ModemState{DeviceID: "marker"}, At: time.Now()}
	prov.events <- marker
	if ev := readRunnerEvent(t, ch); ev.Kind != EventModemRemoved {
		t.Fatalf("duplicate SMSReceived should be suppressed before marker, got %s", ev.Kind)
	}
}

func TestRunnerSMSReceivedDBFailureDoesNotFanout(t *testing.T) {
	store := newTestStore(t)
	prov := newRunnerTestProvider()
	r := NewRunner(prov, store, nil)
	ch, unsub := r.Subscribe(8)
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("runner returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Errorf("runner did not stop")
		}
	})

	bad := SMSRecord{Direction: "inbound", State: "received", Peer: "+1", Text: "missing ext_id"}
	prov.events <- Event{Kind: EventSMSReceived, DeviceID: "dev-runner", Payload: bad, At: time.Now()}
	marker := Event{Kind: EventModemRemoved, DeviceID: "marker", Payload: ModemState{DeviceID: "marker"}, At: time.Now()}
	prov.events <- marker
	if ev := readRunnerEvent(t, ch); ev.Kind != EventModemRemoved {
		t.Fatalf("DB-failed SMSReceived should not fanout, got %s", ev.Kind)
	}
}
