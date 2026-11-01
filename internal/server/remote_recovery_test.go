package server

import "testing"

func TestRemoteLostAckReconcilesAndPendingSurvivesRestart(t *testing.T) {
	h, rx, ctl, p, closeStream := remoteSetup(t)
	defer closeStream()
	submit := func() remoteCommand {
		t.Helper()
		code, raw, _ := h.vrequest("POST", "remote/players/"+p.ID+"/commands", map[string]any{"type": "setVolume", "volume": .25}, ctl.Access, map[string]string{"If-Match": etag(p.Revision)})
		if code != 202 {
			t.Fatal(code, string(raw))
		}
		return decodeData[remoteCommand](t, raw)
	}
	c := submit()
	p.State.Volume = .25
	p.State.Revision++
	p.State.Applied = c.ID
	code, raw, _ := h.vrequest("POST", "remote/receiver/reports", map[string]any{"connectionId": p.Connection, "sequence": 2, "state": p.State}, rx.Access, nil)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	p = decodeData[remotePlayer](t, raw)
	code, raw, _ = h.vrequest("GET", "remote/players/"+p.ID+"/commands/"+c.ID, nil, ctl.Access, nil)
	if decodeData[remoteCommand](t, raw).Status != "applied" {
		t.Fatal("lost ack did not reconcile")
	}
	c = submit()
	closeStream()
	h.restart()
	code, raw, _ = h.vrequest("GET", "remote/players/"+p.ID+"/commands/"+c.ID, nil, ctl.Access, nil)
	if code != 200 || decodeData[remoteCommand](t, raw).Status != "outcome_unknown" {
		t.Fatal("restart replay or false rollback", code, string(raw))
	}
	h.vrequest("POST", "remote/receiver/connections", map[string]any{}, rx.Access, nil)
	code, raw, _ = h.vrequest("GET", "remote/players/"+p.ID+"/commands/"+c.ID, nil, ctl.Access, nil)
	if decodeData[remoteCommand](t, raw).Status != "outcome_unknown" {
		t.Fatal("new generation falsely proved nonexecution")
	}
}

func TestRemotePreconditionsAndBounds(t *testing.T) {
	h, rx, ctl, p, closeStream := remoteSetup(t)
	defer closeStream()
	for _, test := range []struct {
		headers map[string]string
		status  int
	}{{nil, 428}, {map[string]string{"If-Match": "\"v0\""}, 409}} {
		code, raw, _ := h.vrequest("POST", "remote/players/"+p.ID+"/commands", map[string]any{"type": "setVolume", "volume": .2}, ctl.Access, test.headers)
		if code != test.status {
			t.Fatal(code, string(raw))
		}
	}
	for _, volume := range []float64{-1, 2} {
		code, _, _ := h.vrequest("POST", "remote/players/"+p.ID+"/commands", map[string]any{"type": "setVolume", "volume": volume}, ctl.Access, map[string]string{"If-Match": etag(p.Revision)})
		if code != 422 {
			t.Fatal("invalid volume", code)
		}
	}
	p.State.Volume = .1
	code, _, _ := h.vrequest("POST", "remote/receiver/reports", map[string]any{"connectionId": p.Connection, "sequence": 2, "state": p.State}, rx.Access, nil)
	if code != 409 {
		t.Fatal("same-revision physical change accepted")
	}
	code, _, _ = h.vrequest("POST", "remote/players/"+p.ID+"/commands", map[string]any{"type": "setVolume", "volume": .2}, ctl.Access, map[string]string{"If-Match": etag(p.Revision)})
	if code != 202 {
		t.Fatal(code)
	}
	code, _, _ = h.vrequest("POST", "remote/players/"+p.ID+"/commands", map[string]any{"type": "setVolume", "volume": .3}, ctl.Access, map[string]string{"If-Match": etag(p.Revision)})
	if code != 409 {
		t.Fatal("concurrent command accepted")
	}
	oversized := &remotePlayback{Kind: "library", Queue: make([]remoteEntry, 1001)}
	if remoteValidPlayback(oversized, 0) {
		t.Fatal("unbounded queue")
	}
}
