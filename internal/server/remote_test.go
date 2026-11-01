package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

func remoteSetup(t *testing.T) (*harness, credentials, credentials, remotePlayer, func()) {
	t.Helper()
	h := setup(t)
	h.login("alice")
	receiver := h.native("alice", "receiver")
	controller := h.native("alice", "controller")
	code, raw, _ := h.vrequest("PUT", "remote/receiver", map[string]any{"name": "Fixture receiver", "sourceKinds": []string{"library", "radioLive"}, "commands": remoteCommands}, receiver.Access, nil)
	if code != 200 {
		t.Fatalf("register %d %s", code, raw)
	}
	code, raw, _ = h.vrequest("POST", "remote/receiver/connections", map[string]any{}, receiver.Access, nil)
	if code != 200 {
		t.Fatalf("connect %d %s", code, raw)
	}
	p := decodeData[remotePlayer](t, raw)
	req, _ := http.NewRequest("GET", h.app.URL+"/api/v1/remote/receiver/events?connectionId="+p.Connection, nil)
	req.Header.Set("Authorization", "Bearer "+receiver.Access)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	// Consume the stream so bounded server writes cannot be mistaken for receiver failure.
	go func() {
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
		}
	}()
	p.State.Revision = 1
	code, raw, _ = h.vrequest("POST", "remote/receiver/reports", map[string]any{"connectionId": p.Connection, "sequence": 1, "state": p.State}, receiver.Access, nil)
	if code != 200 {
		t.Fatalf("report %d %s", code, raw)
	}
	p = decodeData[remotePlayer](t, raw)
	return h, receiver, controller, p, func() { response.Body.Close() }
}
func TestRemoteIsolationDeduplicationAndObservedState(t *testing.T) {
	h, rx, ctl, p, closeStream := remoteSetup(t)
	defer closeStream()
	bob := h.native("bob", "other")
	code, raw, _ := h.vrequest("GET", "remote/players", nil, bob.Access, nil)
	if code != 200 || string(raw) != "{\"data\":[]}\n" {
		t.Fatalf("isolation %d %s", code, raw)
	}
	code, _, _ = h.vrequest("GET", "remote/players/"+p.ID, nil, bob.Access, nil)
	if code != 404 {
		t.Fatal(code)
	}
	headers := map[string]string{"If-Match": etag(p.Revision), "Idempotency-Key": "11111111-1111-4111-8111-111111111111"}
	body := map[string]any{"type": "setVolume", "volume": .4}
	code, raw, _ = h.vrequest("POST", "remote/players/"+p.ID+"/commands", body, ctl.Access, headers)
	if code != 202 {
		t.Fatalf("submit %d %s", code, raw)
	}
	c := decodeData[remoteCommand](t, raw)
	code, raw, _ = h.vrequest("POST", "remote/players/"+p.ID+"/commands", body, ctl.Access, headers)
	if code != 202 || decodeData[remoteCommand](t, raw).ID != c.ID {
		t.Fatal("retry changed command")
	}
	body["volume"] = .6
	code, _, _ = h.vrequest("POST", "remote/players/"+p.ID+"/commands", body, ctl.Access, headers)
	if code != 409 {
		t.Fatal("changed key accepted")
	}
	code, raw, _ = h.vrequest("GET", "remote/players/"+p.ID, nil, ctl.Access, nil)
	if decodeData[remotePlayer](t, raw).State.Volume != .7 {
		t.Fatal("accepted command changed observed state")
	}
	p.State.Volume = .4
	p.State.Revision++
	p.State.Applied = c.ID
	ack := map[string]any{"connectionId": p.Connection, "status": "applied", "state": p.State}
	code, raw, _ = h.vrequest("POST", "remote/receiver/commands/"+c.ID+"/ack", ack, ctl.Access, nil)
	if code != 404 {
		t.Fatalf("wrong receiver ack %d %s", code, raw)
	}
	for i := 0; i < 2; i++ {
		code, raw, _ = h.vrequest("POST", "remote/receiver/commands/"+c.ID+"/ack", ack, rx.Access, nil)
		if code != 200 {
			t.Fatalf("ack %d %s", code, raw)
		}
	}
	code, raw, _ = h.vrequest("GET", "remote/players/"+p.ID+"/commands/"+c.ID, nil, ctl.Access, nil)
	if decodeData[remoteCommand](t, raw).Status != "applied" {
		t.Fatal(string(raw))
	}
	closeStream()
	h.restart()
	code, raw, _ = h.vrequest("GET", "remote/players/"+p.ID+"/commands/"+c.ID, nil, ctl.Access, nil)
	if code != 200 || decodeData[remoteCommand](t, raw).Status != "applied" {
		t.Fatalf("restart result %d %s", code, raw)
	}
	code, raw, _ = h.vrequest("GET", "remote/players/"+p.ID, nil, ctl.Access, nil)
	if decodeData[remotePlayer](t, raw).Online {
		t.Fatal("restart fabricated presence")
	}
}
func TestRemotePhysicalRaceExpiryAndGenerationFence(t *testing.T) {
	h, rx, ctl, p, closeStream := remoteSetup(t)
	defer closeStream()
	code, raw, _ := h.vrequest("POST", "remote/players/"+p.ID+"/commands", map[string]any{"type": "setVolume", "volume": .2}, ctl.Access, map[string]string{"If-Match": etag(p.Revision)})
	if code != 202 {
		t.Fatal(code, string(raw))
	}
	c := decodeData[remoteCommand](t, raw)
	p.State.Revision++
	p.State.Volume = .3
	report := map[string]any{"connectionId": p.Connection, "sequence": 2, "state": p.State}
	code, raw, _ = h.vrequest("POST", "remote/receiver/reports", report, rx.Access, nil)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	code, raw, _ = h.vrequest("GET", "remote/players/"+p.ID+"/commands/"+c.ID, nil, ctl.Access, nil)
	if decodeData[remoteCommand](t, raw).Status != "rejected" {
		t.Fatal("physical race not fenced")
	}
	report["state"] = remoteState{Transport: "idle", Revision: 2, Volume: .9}
	code, _, _ = h.vrequest("POST", "remote/receiver/reports", report, rx.Access, nil)
	if code != 409 {
		t.Fatal("conflicting report replay")
	}
	code, raw, _ = h.vrequest("GET", "remote/players/"+p.ID, nil, ctl.Access, nil)
	p = decodeData[remotePlayer](t, raw)
	code, raw, _ = h.vrequest("POST", "remote/players/"+p.ID+"/commands", map[string]any{"type": "setVolume", "volume": .5}, ctl.Access, map[string]string{"If-Match": etag(p.Revision)})
	if code != 202 {
		t.Fatal(code, string(raw))
	}
	c = decodeData[remoteCommand](t, raw)
	a := h.app.Config.Handler.(*Server).v1
	a.remote.mu.Lock()
	a.remote.commands[c.ID].Expires = time.Now().Add(-time.Second)
	a.remote.mu.Unlock()
	code, raw, _ = h.vrequest("GET", "remote/players/"+p.ID+"/commands/"+c.ID, nil, ctl.Access, nil)
	if decodeData[remoteCommand](t, raw).Status != "outcome_unknown" {
		t.Fatal("expiry falsely rolled back")
	}
	code, raw, _ = h.vrequest("POST", "remote/receiver/connections", map[string]any{}, rx.Access, nil)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	code, _, _ = h.vrequest("POST", "remote/receiver/reports", report, rx.Access, nil)
	if code != 409 {
		t.Fatal("old generation accepted")
	}
}
func TestRemoteQueueValidation(t *testing.T) {
	p := &remotePlayback{Kind: "library", Queue: []remoteEntry{{EntryID: "a", TrackID: "same"}, {EntryID: "b", TrackID: "same"}}, Current: "a"}
	if !remoteValidPlayback(p, 0) {
		t.Fatal("repeat track rejected")
	}
	p.Queue[1].EntryID = "a"
	if remoteValidPlayback(p, 0) {
		t.Fatal("duplicate occurrence")
	}
	p.Queue[1].EntryID = "b"
	p.Current = "missing"
	if remoteValidPlayback(p, 0) {
		t.Fatal("missing current accepted")
	}
}
func TestRemoteContractFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../fixtures/v1/remote/load.json")
	if err != nil {
		t.Fatal(err)
	}
	var c remoteCommand
	if json.Unmarshal(raw, &c) != nil || !remoteValidPlayback(c.Playback, 0) {
		t.Fatal("contract fixture")
	}
}

func TestRemoteEnrollmentAndAckConflict(t *testing.T) {
	h, rx, ctl, p, closeStream := remoteSetup(t)
	defer closeStream()
	key := map[string]string{"Idempotency-Key": "22222222-2222-4222-8222-222222222222"}
	code, first, _ := h.vrequest("POST", "remote/receiver/connections", map[string]any{}, rx.Access, key)
	if code != 200 {
		t.Fatal(code, string(first))
	}
	code, second, _ := h.vrequest("POST", "remote/receiver/connections", map[string]any{}, rx.Access, key)
	if code != 200 || string(first) != string(second) {
		t.Fatal("enrollment retry changed generation")
	}
	p = decodeData[remotePlayer](t, first)
	// Fenced previous generation must not allow old reports.
	code, _, _ = h.vrequest("POST", "remote/receiver/reports", map[string]any{"connectionId": "old", "sequence": 1, "state": p.State}, rx.Access, nil)
	if code != 409 {
		t.Fatal(code)
	}
	code, _, _ = h.vrequest("POST", "remote/receiver/reports", map[string]any{"connectionId": p.Connection, "sequence": 1, "state": p.State}, ctl.Access, nil)
	if code != 404 {
		t.Fatal("controller impersonated receiver", code)
	}
}
func TestRemoteRevocationClosesControllerStream(t *testing.T) {
	h, rx, ctl, _, closeStream := remoteSetup(t)
	defer closeStream()
	req, _ := http.NewRequest("GET", h.app.URL+"/api/v1/remote/events", nil)
	req.Header.Set("Authorization", "Bearer "+ctl.Access)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
		}
	}()
	code, raw, _ := h.vrequest("DELETE", "auth/devices/"+ctl.Device, nil, rx.Access, nil)
	if code != 204 {
		t.Fatal(code, string(raw))
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("revocation did not actively close SSE")
	}
	code, _, _ = h.vrequest("GET", "remote/players", nil, ctl.Access, nil)
	if code != 401 {
		t.Fatal("revoked control still valid")
	}
}
func TestRemoteRejectsFalseStartupAcknowledgement(t *testing.T) {
	h, rx, ctl, p, closeStream := remoteSetup(t)
	defer closeStream()
	playback := &remotePlayback{Kind: "library", Queue: []remoteEntry{{EntryID: "a", TrackID: "0-0"}}, Current: "a", Play: true}
	code, raw, _ := h.vrequest("POST", "remote/players/"+p.ID+"/commands", map[string]any{"type": "load", "playback": playback}, ctl.Access, map[string]string{"If-Match": etag(p.Revision)})
	if code != 202 {
		t.Fatal(code, string(raw))
	}
	c := decodeData[remoteCommand](t, raw)
	p.State.Playback = playback
	p.State.Transport = "buffering"
	p.State.Revision++
	p.State.Applied = c.ID
	code, _, _ = h.vrequest("POST", "remote/receiver/commands/"+c.ID+"/ack", map[string]any{"connectionId": p.Connection, "status": "applied", "state": p.State}, rx.Access, nil)
	if code != 409 {
		t.Fatal("buffering treated as success")
	}
	p.State.Transport = "playing"
	code, raw, _ = h.vrequest("POST", "remote/receiver/commands/"+c.ID+"/ack", map[string]any{"connectionId": p.Connection, "status": "applied", "state": p.State}, rx.Access, nil)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
}
