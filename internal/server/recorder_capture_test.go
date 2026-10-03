package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type fixtureRadioTransport struct {
	client *http.Client
	target string
}

func (f fixtureRadioTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *copy.URL
	u.Scheme = "http"
	u.Host = strings.TrimPrefix(f.target, "http://")
	copy.URL = &u
	return f.client.Transport.RoundTrip(copy)
}

func TestRecorderRealCaptureRestartRangesAndChecksum(t *testing.T) {
	if !mediaToolsAvailable() {
		t.Fatal("FFmpeg and FFprobe required for recorder acceptance")
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "failed") {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.(http.Flusher).Flush()
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-re", "-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000", "-c:a", "libmp3lame", "-b:a", "128k", "-f", "mp3", "pipe:1")
		cmd.Stdout = &flushWriter{w}
		_ = cmd.Run()
	}))
	defer fixture.Close()
	h := setup(t)
	h.login("alice")
	h.app.Close()
	h.app.Config.Handler.(interface{ Close() }).Close()
	cfg := Config{DataDir: h.dir, SegmentSeconds: 2, RadioClient: &http.Client{Transport: fixtureRadioTransport{fixture.Client(), fixture.URL}}}
	handler, e := New(cfg, fstest.MapFS{"index.html": {Data: []byte("fixture")}})
	if e != nil {
		t.Fatal(e)
	}
	h.app = httptest.NewServer(handler)
	t.Cleanup(h.app.Close)
	t.Cleanup(handler.(interface{ Close() }).Close)
	c := h.native("alice", "phone")
	code, b, _ := h.vrequest("POST", "radio/stations", map[string]any{"name": "Tone", "streamURL": "https://radio.fixture.invalid/live", "preset": true}, c.Access, nil)
	if code != 201 {
		t.Fatal(code, string(b))
	}
	station := decodeData[nativeStation](t, b)
	code, b, _ = h.vrequest("POST", "flight-recorder/sessions", map[string]any{"name": "Real capture", "stationIds": []string{station.ID}, "durationMs": 14000}, c.Access, nil)
	if code != 201 {
		t.Fatal(code, string(b))
	}
	j := decodeData[recordingJob](t, b)
	deadline := time.Now().Add(9 * time.Second)
	for time.Now().Before(deadline) {
		code, b, _ = h.vrequest("GET", "flight-recorder/sessions/"+j.ID, nil, c.Access, nil)
		j = decodeData[recordingJob](t, b)
		if j.Bytes > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if j.Bytes == 0 {
		t.Fatal("capture produced no published bytes", string(b))
	}
	h.vrequest("POST", "auth/logout", nil, c.Access, nil)
	h.app.Close()
	handler.(interface{ Close() }).Close()
	handler, e = New(cfg, fstest.MapFS{"index.html": {Data: []byte("fixture")}})
	if e != nil {
		t.Fatal(e)
	}
	h.app = httptest.NewServer(handler)
	t.Cleanup(h.app.Close)
	t.Cleanup(handler.(interface{ Close() }).Close)
	c = h.native("alice", "returned phone")
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		code, b, _ = h.vrequest("GET", "flight-recorder/sessions/"+j.ID, nil, c.Access, nil)
		j = decodeData[recordingJob](t, b)
		if finalState(j.State) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if j.State != "partial" && j.State != "complete" {
		t.Fatalf("capture failed: %s %s", j.State, b)
	}
	if j.Ends.Sub(j.Starts) != 14*time.Second {
		t.Fatal("restart shifted deadline")
	}
	code, b, headers := h.vrequest("GET", "flight-recorder/sessions/"+j.ID+"/manifest", nil, c.Access, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	manifest := decodeData[recordingManifest](t, b)
	if len(manifest.Tracks[0].Segments) == 0 {
		t.Fatal("manifest empty")
	}
	code, _, _ = h.vrequest("GET", "flight-recorder/sessions/"+j.ID+"/manifest", nil, c.Access, map[string]string{"If-None-Match": headers.Get("ETag")})
	if code != 304 {
		t.Fatal("conditional manifest failed")
	}
	asset := manifest.Tracks[0].Segments[0]
	code, b, _ = h.vrequest("POST", "media/leases", map[string]string{"kind": "recordingSegment", "resourceId": asset.AssetID}, c.Access, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	lease := decodeData[map[string]any](t, b)
	url := h.app.URL + lease["url"].(string)
	for _, method := range []string{"GET", "HEAD"} {
		req, _ := http.NewRequest(method, url, nil)
		req.Header.Set("Range", "bytes=0-31")
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 206 || method == "GET" && len(raw) != 32 {
			t.Fatal("asset range", method, res.StatusCode, len(raw))
		}
	}
	file, e := os.ReadFile(filepath.Join(h.dir, "recordings", asset.AssetID+".m4a"))
	if e != nil || digest(string(file)) != asset.Hash {
		t.Fatal("asset checksum invalid")
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Range", "bytes=9999999999-")
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 416 {
		t.Fatal("invalid range accepted")
	}
	code, _, _ = h.vrequest("DELETE", "flight-recorder/sessions/"+j.ID, nil, c.Access, map[string]string{"If-Match": etag(j.Version)})
	if code != 204 {
		t.Fatal("delete failed", code)
	}
	res, e = http.Get(url)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 410 {
		t.Fatal("tombstone did not expire asset", res.StatusCode)
	}
}

type flushWriter struct{ http.ResponseWriter }

func (f *flushWriter) Write(b []byte) (int, error) {
	n, e := f.ResponseWriter.Write(b)
	f.ResponseWriter.(http.Flusher).Flush()
	return n, e
}
func TestRecorderDiskReserveRejection(t *testing.T) {
	h := setup(t)
	s := h.app.Config.Handler.(*Server)
	s.v1.mu.Lock()
	s.cfg.ReserveBytes = 1 << 60
	v, e := s.v1.storage()
	s.v1.mu.Unlock()
	if e != nil || v.Available != 0 {
		t.Fatal("disk reserve ignored", v, e)
	}
}
