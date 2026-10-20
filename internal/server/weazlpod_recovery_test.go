package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPodAdmissionCancellationAndRecovery(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "pod")
	s := h.app.Config.Handler.(*Server)
	a := s.v1
	original := s.upstream.Transport
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer upstream.Close()
	s.upstream.Transport = podDownloadTransport{base: original, target: upstream.URL}
	create := func(id string) (int, []byte, http.Header) {
		return h.vrequest("POST", "media/leases", map[string]string{"kind": "trackStream", "resourceId": id, "quality": podProfile}, c.Access, nil)
	}
	var ids []string
	for _, resource := range []string{"0-0", "0-1"} {
		code, b, _ := create(resource)
		if code != 202 {
			t.Fatal(code, string(b))
		}
		ids = append(ids, decodeData[podPreparation](t, b).ID)
	}
	code, b, headers := create("0-2")
	if code != 429 || headers.Get("Retry-After") == "" {
		t.Fatal("unbounded workers", code, string(b))
	}
	start := time.Now()
	code, b, _ = h.vrequest("GET", "me", nil, c.Access, nil)
	if code != 200 || time.Since(start) > time.Second {
		t.Fatal("global API blocked", code, string(b))
	}
	code, b, _ = h.vrequest("DELETE", "media/preparations/"+ids[0], nil, c.Access, nil)
	if code != 204 {
		t.Fatal(code, string(b))
	}
	deadline := time.Now().Add(5 * time.Second)
	for a.pod.reserved.Load() != podReservation {
		if time.Now().After(deadline) {
			t.Fatal("cancellation leaked reservation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	code, b, _ = h.vrequest("GET", "media/preparations/"+ids[0], nil, c.Access, nil)
	if !bytes.Contains(b, []byte("cancelled")) {
		t.Fatal(code, string(b))
	}
	// Force a real server lifecycle with an input still in flight. No unfinished lease survives.
	h.restart()
	code, b, _ = h.vrequest("GET", "media/preparations/"+ids[1], nil, c.Access, nil)
	if code != 404 {
		t.Fatal("interrupted job pretends ready", code, string(b))
	}
	a = h.app.Config.Handler.(*Server).v1
	if a.pod.reserved.Load() != 0 {
		t.Fatal("restart leaked reservation")
	}
	entries, err := os.ReadDir(a.pod.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "staging-") {
			t.Fatal("orphan staging")
		}
	}
	// Unpublished and orphaned artifacts are disposable, while masters survive recovery.
	_ = os.WriteFile(filepath.Join(a.pod.root, "orphan.mp3"), []byte("partial"), 0600)
	_ = os.MkdirAll(filepath.Join(a.pod.root, "staging-interrupted"), 0700)
	_ = os.WriteFile(filepath.Join(h.dir, "recordings", "authoritative.m4a"), []byte("master"), 0600)
	h.restart()
	a = h.app.Config.Handler.(*Server).v1
	entries, _ = os.ReadDir(a.pod.root)
	if len(entries) != 0 {
		t.Fatal("recovery left orphan files", entries)
	}
	if _, err = os.Stat(filepath.Join(h.dir, "recordings", "authoritative.m4a")); err != nil {
		t.Fatal("master removed", err)
	}
}
func TestPodStorageEvictionExpiryAndUnavailableEncoder(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "pod")
	s := h.app.Config.Handler.(*Server)
	a := s.v1
	body := map[string]string{"kind": "trackDownload", "resourceId": "0-0", "quality": podProfile}
	code, b, _ := h.vrequest("POST", "media/leases", body, c.Access, nil)
	d := podPoll(t, h, c.Access, code, b)
	id := d["representationId"].(string)
	leaseID := strings.TrimPrefix(strings.Split(d["url"].(string), "?")[0], "/api/v1/media/assets/")
	var ml mediaLease
	if err := a.get("lease", leaseID, accountID("alice"), &ml); err != nil {
		t.Fatal(err)
	}
	expires := ml.Expires
	ml.Expires = a.now().Add(-time.Second)
	_ = a.put("lease", ml.ID, accountID("alice"), ml)
	code, _, _ = podGet(t, h, "GET", d["url"].(string), nil)
	if code != 410 {
		t.Fatal("expired MP3 lease", code)
	}
	ml.Expires = expires
	_ = a.put("lease", ml.ID, accountID("alice"), ml)

	a.pod.mu.Lock()
	rep := a.pod.reps[id]
	rep.Pin = a.now().Add(-time.Hour)
	a.pod.reps[id] = rep
	a.pod.mu.Unlock()
	// Open-reader protection is independent of lease pinning; admission must fail, not evict it.
	s.cfg.TranscodeBudgetBytes = podReservation
	a.mu.Lock()
	a.pod.mu.Lock()
	a.pod.readers[id] = 1
	ok := a.podReserve()
	a.pod.mu.Unlock()
	a.mu.Unlock()
	if ok {
		t.Fatal("active reader evicted")
	}
	a.mu.Lock()
	a.pod.mu.Lock()
	a.pod.readers[id] = 0
	ok = a.podReserve()
	a.pod.mu.Unlock()
	a.mu.Unlock()
	if !ok {
		t.Fatal("unused derivative not evicted")
	}
	a.pod.reserved.Add(-podReservation)
	if _, err := os.Stat(a.pod.path(id)); !os.IsNotExist(err) {
		t.Fatal("eviction left bytes")
	}
	code, _, _ = podGet(t, h, "GET", d["url"].(string), nil)
	if code != 410 {
		t.Fatal("evicted lease", code)
	}
	s.cfg.TranscodeBudgetBytes = podReservation - 1
	code, b, headers := h.vrequest("POST", "media/leases", body, c.Access, nil)
	if code != 507 || !bytes.Contains(b, []byte("storage_full")) || headers.Get("Retry-After") == "" {
		t.Fatal("quota", code, string(b))
	}
	a.pod.encoder = ""
	code, b, _ = h.vrequest("POST", "media/leases", body, c.Access, nil)
	if code != 422 || !bytes.Contains(b, []byte("unsupported")) {
		t.Fatal("encoder absent fell back", code, string(b))
	}
	code, b, _ = h.vrequest("GET", "capabilities", nil, c.Access, nil)
	if code != 200 || !bytes.Contains(b, []byte(`"finiteLibrary":false`)) {
		t.Fatal("absent encoder advertised", code, string(b))
	}
}
func TestPodFailedSourceNeverLeasedAndEncoderCancellation(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "pod")
	s := h.app.Config.Handler.(*Server)
	original := s.upstream.Transport
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "corrupt audio") }))
	defer origin.Close()
	s.upstream.Transport = podDownloadTransport{base: original, target: origin.URL}
	code, b, _ := h.vrequest("POST", "media/leases", map[string]string{"kind": "trackStream", "resourceId": "0-0", "quality": podProfile}, c.Access, nil)
	if code != 202 {
		t.Fatal(code, string(b))
	}
	id := decodeData[podPreparation](t, b).ID
	deadline := time.Now().Add(10 * time.Second)
	for code == 202 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		code, b, _ = h.vrequest("GET", "media/preparations/"+id, nil, c.Access, nil)
	}
	if code != 200 || !bytes.Contains(b, []byte("transcode_failed")) || bytes.Contains(b, []byte(`"url"`)) {
		t.Fatal("bad source leased", code, string(b))
	}
	// A killed encoder cannot publish even if output bytes were left behind.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := filepath.Join(t.TempDir(), "cancelled.mp3")
	if err := podEncodeCommand(ctx, filepath.Join("..", "..", "fixtures", "v1", "media", "aac-lc-160k-stereo.m4a"), out).Run(); err == nil {
		t.Fatal("cancelled encode succeeded")
	}
}
