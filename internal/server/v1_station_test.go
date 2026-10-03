package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeStationsVersionsAndLegacyIsolation(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "phone")
	code, b, head := h.vrequest("GET", "radio/stations", nil, c.Access, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	rows := decodeData[[]nativeStation](t, b)
	id := rows[0].ID
	tag := head.Get("ETag")
	code, _, _ = h.vrequest("PATCH", "radio/stations/"+id, map[string]string{"name": "Fixture rename"}, c.Access, nil)
	if code != 428 {
		t.Fatal("missing version accepted")
	}
	code, b, _ = h.vrequest("PATCH", "radio/stations/"+id, map[string]string{"name": "Fixture rename"}, c.Access, map[string]string{"If-Match": etag(rows[0].Version)})
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, _, _ = h.vrequest("PUT", "radio/presets", map[string]any{"stationIds": []string{id}}, c.Access, map[string]string{"If-Match": tag})
	if code != 409 {
		t.Fatal("stale collection accepted")
	}
	code, _, res := h.request("POST", "/api/login", loginInput{"alice", "test-password"}, nil)
	if code != 200 {
		t.Fatal(code)
	}
	cookie := res.Cookies()[0]
	code, b, _ = h.request("GET", "/api/state", nil, cookie)
	if code != 200 || !strings.Contains(string(b), "Fixture rename") {
		t.Fatal("browser missed native edit")
	}
	var saved userState
	_ = json.Unmarshal(b, &saved)
	saved.Queue = []json.RawMessage{json.RawMessage(`{"id":"0-0","title":"Queue fixture"}`)}
	code, _, _ = h.request("PUT", "/api/state", saved, cookie)
	if code != 200 {
		t.Fatal(code)
	}
	saved.StationsVersion--
	code, _, _ = h.request("PUT", "/api/state", saved, cookie)
	if code != 409 {
		t.Fatal("stale legacy write accepted")
	}
	code, b, _ = h.vrequest("PUT", "queue", map[string]any{"queue": []map[string]string{{"id": "0-0", "title": "Device queue"}}, "current": nil}, c.Access, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	other := h.native("alice", "tablet")
	code, b, _ = h.vrequest("GET", "queue", nil, other.Access, nil)
	if code != 200 || strings.Contains(string(b), "Device queue") {
		t.Fatal("queue crossed devices")
	}
}
