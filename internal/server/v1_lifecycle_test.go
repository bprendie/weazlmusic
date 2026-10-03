package server

import (
	"bytes"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeClockLeaseExpiryRenewalAndOfflineRefresh(t *testing.T) {
	h := setup(t)
	h.login("alice")
	a := h.app.Config.Handler.(*Server).v1
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	a.mu.Lock()
	a.now = func() time.Time { return time.Unix(0, clock.Load()) }
	a.mu.Unlock()
	first := h.native("alice", "first")
	second := h.native("alice", "second")
	code, b, _ := h.vrequest("POST", "media/leases", map[string]string{"kind": "trackOriginal", "resourceId": "0-0"}, first.Access, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	lease := decodeData[map[string]any](t, b)
	mediaURL := h.app.URL + lease["url"].(string)
	clock.Add(int64(16 * time.Minute))
	code, _, _ = h.vrequest("GET", "me", nil, first.Access, nil)
	if code != 401 {
		t.Fatal("access expiry ignored")
	}
	refresh := func(c credentials) credentials {
		code, body, _ := h.vrequest("POST", "auth/refresh", map[string]string{"refreshToken": c.Refresh}, "", nil)
		if code != 200 {
			t.Fatal(code, string(body))
		}
		return decodeData[credentials](t, body)
	}
	first = refresh(first)
	clock.Add(int64(12 * time.Hour))
	response, err := http.Get(mediaURL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 410 {
		t.Fatal("lease expiry ignored", response.StatusCode)
	}
	first = refresh(first)
	second = refresh(second)
	code, b, _ = h.vrequest("POST", "media/leases", map[string]string{"kind": "trackOriginal", "resourceId": "0-0"}, first.Access, nil)
	if code != 200 {
		t.Fatal("renewal failed", code, string(b))
	}
	lease = decodeData[map[string]any](t, b)
	mediaURL = h.app.URL + lease["url"].(string)
	code, _, _ = h.vrequest("DELETE", "auth/devices/"+first.Device, nil, second.Access, nil)
	if code != 204 {
		t.Fatal(code)
	}
	response, err = http.Get(mediaURL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("revoked device lease still usable")
	}
	h.navHTTP.Close()
	second = refresh(second)
	code, b, _ = h.vrequest("GET", "me", nil, second.Access, nil)
	if code != 200 || !bytes.Contains(b, []byte(second.Account)) {
		t.Fatal("offline refresh lost local identity")
	}
}

func TestRecorderInjectedClockMissedOnceAndRetention(t *testing.T) {
	h := setup(t)
	a := h.app.Config.Handler.(*Server).v1
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	a.mu.Lock()
	a.now = func() time.Time { return now }
	se := &session{User: "alice"}
	job := a.makeJob("clock-missed", "", scheduleInput{Starts: now.Add(time.Hour), Ends: now.Add(2 * time.Hour)}, se, []nativeStation{{ID: "station"}})
	if err := a.put("job", job.ID, accountID(se.User), job); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Hour)
	a.mu.Unlock()
	a.tick()
	a.tick()
	a.mu.Lock()
	defer a.mu.Unlock()
	var missed recordingJob
	if err := a.get("job", job.ID, accountID(se.User), &missed); err != nil || missed.State != "missed" || missed.Version != job.Version+1 {
		t.Fatal("duplicate firing or shifted window", err, missed)
	}
	a.s.cfg.RetentionDays = 1
	now = now.Add(48 * time.Hour)
	a.retention()
	if err := a.get("job", job.ID, accountID(se.User), &missed); err != nil || !missed.Tombstone {
		t.Fatal("explicit retention ignored")
	}
}
