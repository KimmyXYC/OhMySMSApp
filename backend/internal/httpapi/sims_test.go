package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

func TestDeleteUnusedSIMAPI(t *testing.T) {
	provider := modem.NewMockProvider(nil)
	srv, tok, store := setupWithProviderAndStore(t, provider)
	ctx := context.Background()

	modemID, err := store.UpsertModem(ctx, modem.ModemState{DeviceID: "unused-sim-modem", IMEI: "333456789012345"})
	if err != nil {
		t.Fatal(err)
	}
	simID, err := store.UpsertSim(ctx, modem.SimState{ICCID: "8986000000000000099"}, modemID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UnbindModem(ctx, modemID); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/sims/"+strconv.FormatInt(simID, 10), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 deleting unused sim, got %d", resp.StatusCode)
	}
}
