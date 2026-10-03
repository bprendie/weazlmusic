package server

import (
	"bytes"
	"net/http"
	"testing"
	"time"
)

func TestNativeMoodCuratorAndEvents(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "phone")
	other := h.native("bob", "phone")
	code, b, _ := h.vrequest("PUT", "preferences/curator", map[string]any{"provider": "ollama", "url": h.upstream, "model": "fixture-model", "apiKey": "private-key"}, c.Access, nil)
	if code != 200 || bytes.Contains(b, []byte("private-key")) {
		t.Fatal("curator settings", code, string(b))
	}
	code, b, _ = h.vrequest("POST", "mood/jobs", map[string]string{"seedTrackId": "0-0"}, c.Access, nil)
	if code != 201 {
		t.Fatal("Mood create", code, string(b))
	}
	job := decodeData[map[string]string](t, b)
	id := job["jobId"]
	code, _, _ = h.vrequest("GET", "mood/jobs/"+id, nil, other.Access, nil)
	if code != 404 {
		t.Fatal("Mood ownership failed")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		code, b, _ = h.vrequest("GET", "mood/jobs/"+id, nil, c.Access, nil)
		j := decodeData[nativeMood](t, b)
		if j.State == "completed" {
			if len(j.Events) != 21 || j.Events[0].Type != "progress" || j.Events[len(j.Events)-1].Type != "completed" {
				t.Fatal("Mood event shape", string(b))
			}
			req, _ := http.NewRequest("GET", h.app.URL+"/api/v1/mood/jobs/"+id+"/events", nil)
			req.Header.Set("Authorization", "Bearer "+c.Access)
			req.Header.Set("Last-Event-ID", "20")
			res, e := http.DefaultClient.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			res.Body.Close()
			if res.Header.Get("Content-Type") != "text/event-stream" {
				t.Fatal("native SSE unavailable")
			}
			return
		}
		if j.State == "failed" {
			t.Fatal("Mood failed", string(b))
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("Mood did not complete")
}
