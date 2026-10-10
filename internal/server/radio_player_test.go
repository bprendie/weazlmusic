package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestRadioProbeOverlapsAudioAndReopensWithMetadataListener(t *testing.T) {
	var calls atomic.Int32
	releaseProbe := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first := calls.Add(1) == 1
		w.Header().Set("Content-Type", "audio/mpeg")
		w.(http.Flusher).Flush()
		if first {
			select {
			case <-releaseProbe:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = w.Write(bytes.Repeat([]byte("A"), 32*1024))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := newRadioHub()
	se := &session{Expires: time.Now().Add(time.Minute), ctx: ctx, radio: hub}
	s := &Server{radio: upstream.Client()}
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.radioStream(w, r, se) }))
	defer app.Close()
	id := "0123456789abcdef0123456789abcdef"
	// An SSE listener keeps this feed alive across audio disconnects.
	_, release, err := hub.acquire(id)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	client := &http.Client{Timeout: 5 * time.Second}
	get := func(probe bool) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", app.URL+"/?"+url.Values{"url": {upstream.URL}, "playback": {id}}.Encode(), nil)
		if probe {
			req.Header.Set("Range", "bytes=0-1")
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	probe := get(true)
	defer probe.Body.Close()
	audio := get(false)
	defer audio.Body.Close()
	if probe.StatusCode != 200 || audio.StatusCode != 200 {
		t.Fatal("probe claimed playback", probe.StatusCode, audio.StatusCode)
	}
	buf := make([]byte, 8)
	if _, err := io.ReadFull(audio.Body, buf); err != nil || string(buf) != "AAAAAAAA" {
		t.Fatal("audio relay failed", err)
	}
	duplicate := get(false)
	duplicate.Body.Close()
	if duplicate.StatusCode != 409 {
		t.Fatal("two actual audio owners accepted", duplicate.StatusCode)
	}
	invalid, _ := http.NewRequest(http.MethodGet, app.URL+"/?"+url.Values{"url": {upstream.URL}, "playback": {"bad"}}.Encode(), nil)
	invalid.Header.Set("Range", " bytes=0-1 ")
	invalidResponse, err := client.Do(invalid)
	if err != nil {
		t.Fatal(err)
	}
	invalidResponse.Body.Close()
	if invalidResponse.StatusCode != 400 {
		t.Fatal("invalid probe playback ID accepted", invalidResponse.StatusCode)
	}
	nonProbe, _ := http.NewRequest(http.MethodGet, app.URL+"/?"+url.Values{"url": {upstream.URL}, "playback": {id}}.Encode(), nil)
	nonProbe.Header.Set("Range", "bytes=0-2")
	nonProbeResponse, err := client.Do(nonProbe)
	if err != nil {
		t.Fatal(err)
	}
	nonProbeResponse.Body.Close()
	if nonProbeResponse.StatusCode != 409 {
		t.Fatal("non-probe range bypassed playback owner", nonProbeResponse.StatusCode)
	}
	close(releaseProbe)
	bounded, err := io.ReadAll(probe.Body)
	if err != nil || len(bounded) != 16*1024 || probe.ContentLength != -1 {
		t.Fatal("probe must be bounded without a static file size", len(bounded), probe.ContentLength, err)
	}
	audio.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reopened := get(false)
		reopened.Body.Close()
		if reopened.StatusCode == 200 {
			return
		}
		if reopened.StatusCode != 409 {
			t.Fatal("unexpected reopen failure", reopened.StatusCode)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("metadata listener permanently retained an active audio owner")
}

// Exercise the native lease and media route, which rewrites the authorized
// station into the relay request. No account or station fixture reaches prod.
func TestNativeRadioLeaseProbeAndAudio(t *testing.T) {
	releaseProbe := make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first := calls.Add(1) == 1
		w.Header().Set("Content-Type", "audio/mpeg")
		w.(http.Flusher).Flush()
		if first {
			select {
			case <-releaseProbe:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = w.Write(bytes.Repeat([]byte("B"), 32*1024))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	h := setup(t)
	h.app.Config.Handler.(*Server).radio = upstream.Client()
	h.login("alice")
	device := h.native("alice", "radio-probe-test")
	code, body, _ := h.vrequest("POST", "radio/stations", map[string]any{"name": "Probe fixture", "streamURL": upstream.URL, "preset": true}, device.Access, nil)
	if code != 201 {
		t.Fatal("station setup", code, string(body))
	}
	station := decodeData[nativeStation](t, body)
	code, body, _ = h.vrequest("POST", "media/leases", map[string]string{"kind": "radioLive", "resourceId": station.ID}, device.Access, nil)
	if code != 200 {
		t.Fatal("lease", code, string(body))
	}
	lease := decodeData[struct {
		URL string `json:"url"`
	}](t, body)
	client := &http.Client{Timeout: 5 * time.Second}
	get := func(rng string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, h.app.URL+lease.URL, nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	probe := get("bytes=0-1")
	defer probe.Body.Close()
	audio := get("")
	defer audio.Body.Close()
	if probe.StatusCode != 200 || audio.StatusCode != 200 {
		t.Fatal("leased probe/audio", probe.StatusCode, audio.StatusCode)
	}
	buf := make([]byte, 8)
	if _, err := io.ReadFull(audio.Body, buf); err != nil || string(buf) != "BBBBBBBB" {
		t.Fatal("leased audio", err, string(buf))
	}
	duplicate := get("")
	duplicate.Body.Close()
	if duplicate.StatusCode != 409 {
		t.Fatal("duplicate leased audio", duplicate.StatusCode)
	}
	close(releaseProbe)
	data, err := io.ReadAll(probe.Body)
	if err != nil || len(data) != 16*1024 || probe.ContentLength != -1 || probe.Header.Get("Content-Range") != "" || probe.Header.Get("Accept-Ranges") != "" {
		t.Fatal("leased probe headers/body", len(data), probe.ContentLength, err)
	}
}
