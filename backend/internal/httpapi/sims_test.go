package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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

func TestSIMMSISDNOverrideAPI_SetClearNotFoundAndInvalidJSON(t *testing.T) {
	provider := modem.NewMockProvider(nil)
	srv, tok, store := setupWithProviderAndStore(t, provider)
	ctx := context.Background()

	modemID, err := store.UpsertModem(ctx, modem.ModemState{DeviceID: "api-msisdn-modem", IMEI: "333456789012346"})
	if err != nil {
		t.Fatal(err)
	}
	simID, err := store.UpsertSim(ctx, modem.SimState{ICCID: "8986000000000000199", MSISDN: "+10000000001"}, modemID)
	if err != nil {
		t.Fatal(err)
	}

	doPut := func(id int64, body []byte) (*http.Response, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/sims/"+strconv.FormatInt(id, 10)+"/msisdn", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var row map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&row)
		resp.Body.Close()
		return resp, row
	}

	raw, _ := json.Marshal(map[string]string{"msisdn": " 491701234567 "})
	resp, row := doPut(simID, raw)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set msisdn override status=%d body=%v", resp.StatusCode, row)
	}
	if row["msisdn"] != "+491701234567" || row["msisdn_override"] != "+491701234567" {
		t.Fatalf("expected normalized override in response, got %v", row)
	}

	badBodies := map[string][]byte{
		"missing msisdn": []byte(`{}`),
		"null msisdn":    []byte(`{"msisdn":null}`),
		"too long":       []byte(`{"msisdn":"` + strings.Repeat("1", 33) + `"}`),
		"control char":   []byte("{\"msisdn\":\"+12\\u000023\"}"),
	}
	for name, body := range badBodies {
		resp, _ = doPut(simID, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", name, resp.StatusCode)
		}
	}
	persisted, err := store.GetSIMByID(ctx, simID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.MSISDNOverride == nil || *persisted.MSISDNOverride != "+491701234567" {
		t.Fatalf("expected invalid bodies not to clear override, got %#v", persisted.MSISDNOverride)
	}

	// 清空后 msisdn 回退到硬件 OwnNumbers，msisdn_override 省略或为 nil 都可接受。
	raw, _ = json.Marshal(map[string]string{"msisdn": "   "})
	resp, row = doPut(simID, raw)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear msisdn override status=%d body=%v", resp.StatusCode, row)
	}
	if row["msisdn"] != "+10000000001" {
		t.Fatalf("expected fallback hardware msisdn, got %v", row["msisdn"])
	}
	if v, ok := row["msisdn_override"]; ok && v != nil {
		t.Fatalf("expected msisdn_override omitted or nil, got %v", v)
	}

	resp, _ = doPut(simID+9999, []byte(`{"msisdn":"+1"}`))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing sim, got %d", resp.StatusCode)
	}

	resp, _ = doPut(simID, []byte(`{"msisdn":`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid json, got %d", resp.StatusCode)
	}
}
