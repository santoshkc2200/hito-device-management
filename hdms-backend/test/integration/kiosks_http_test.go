//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPKiosksLifecycle(t *testing.T) {
	h := newTestHarness(t)

	// 1. Create kiosk
	loc := "Main Entrance"
	resp := h.post(t, "/v1/kiosks", gen.CreateKioskRequest{
		Name:     "Kiosk Alpha",
		Location: &loc,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CreateKiosk status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control header = %s, want no-store", resp.Header.Get("Cache-Control"))
	}
	created := decodeBody[gen.KioskWithToken](t, resp)
	if created.Token == "" || created.Name != "Kiosk Alpha" {
		t.Fatalf("Unexpected created kiosk: %+v", created)
	}

	// 2. List kiosks
	resp = h.get(t, "/v1/kiosks")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListKiosks status = %d", resp.StatusCode)
	}
	list := decodeBody[gen.KioskList](t, resp)
	if len(list.Items) != 1 || list.Items[0].Id != created.Id {
		t.Fatalf("ListKiosks unexpected items: %+v", list.Items)
	}

	// 3. Rotate token
	resp = h.post(t, "/v1/kiosks/"+created.Id+"/rotate-token", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("RotateKioskToken status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control header = %s, want no-store", resp.Header.Get("Cache-Control"))
	}
	rotated := decodeBody[gen.KioskWithToken](t, resp)
	if rotated.Token == "" || rotated.Token == created.Token {
		t.Fatalf("Rotated token should be fresh: %s vs %s", rotated.Token, created.Token)
	}

	// 4. Generate pairing code
	resp = h.post(t, "/v1/kiosks/"+created.Id+"/pairing-code", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CreateKioskPairingCode status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control header = %s, want no-store", resp.Header.Get("Cache-Control"))
	}
	pairing := decodeBody[gen.KioskPairingCode](t, resp)
	if pairing.Code == "" {
		t.Fatal("Pairing code is empty")
	}

	// 5. Redeem pairing code (unauthenticated)
	anonClient := &http.Client{}
	reqBody, _ := json.Marshal(gen.PairKioskRequest{Code: pairing.Code})
	pairReq, _ := http.NewRequest(http.MethodPost, h.server.URL+"/v1/kiosks/pair", bytes.NewReader(reqBody))
	pairReq.Header.Set("Content-Type", "application/json")
	pairResp, err := anonClient.Do(pairReq)
	if err != nil {
		t.Fatalf("Do PairKiosk: %v", err)
	}
	defer pairResp.Body.Close()
	if pairResp.StatusCode != http.StatusOK {
		t.Fatalf("PairKiosk status = %d, want 200", pairResp.StatusCode)
	}
	if pairResp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control header = %s, want no-store", pairResp.Header.Get("Cache-Control"))
	}
	var pairResult gen.PairKioskResponse
	if err := json.NewDecoder(pairResp.Body).Decode(&pairResult); err != nil {
		t.Fatalf("Decode pair result: %v", err)
	}
	if pairResult.KioskId != created.Id || pairResult.Token == "" {
		t.Fatalf("Unexpected pair result: %+v", pairResult)
	}

	// 6. Disable kiosk
	resp = h.post(t, "/v1/kiosks/"+created.Id+"/disable", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DisableKiosk status = %d", resp.StatusCode)
	}
	disabled := decodeBody[gen.Kiosk](t, resp)
	if disabled.Status != gen.KioskStatusDisabled {
		t.Fatalf("Disabled kiosk status = %s, want disabled", disabled.Status)
	}
}
