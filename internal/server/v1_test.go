package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
)

var requestSequence atomic.Int64

func (h *harness) vrequest(method, path string, body any, token string, headers map[string]string) (int, []byte, http.Header) {
	h.t.Helper()
	b, _ := json.Marshal(body)
	if body == nil {
		b = nil
	}
	req, _ := http.NewRequest(method, h.app.URL+"/api/v1/"+path, bytes.NewReader(b))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	if method != "GET" && method != "HEAD" {
		req.Header.Set("Idempotency-Key", fmt.Sprintf("00000000-0000-4000-8000-%012d", requestSequence.Add(1)))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		h.t.Fatal(e)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw, res.Header
}
func decodeData[T any](t *testing.T, b []byte) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if e := json.Unmarshal(b, &envelope); e != nil {
		t.Fatalf("JSON: %s %v", b, e)
	}
	return envelope.Data
}
func (h *harness) native(user, device string) credentials {
	h.t.Helper()
	code, b, _ := h.vrequest("POST", "auth/login", map[string]string{"username": user, "password": "test-password", "deviceName": device, "clientId": device}, "", nil)
	if code != 200 {
		h.t.Fatalf("native login: %d %s", code, b)
	}
	return decodeData[credentials](h.t, b)
}
func (h *harness) restart() {
	h.t.Helper()
	h.app.Close()
	h.app.Config.Handler.(interface{ Close() }).Close()
	handler, e := New(Config{DataDir: h.dir}, fstest.MapFS{"index.html": {Data: []byte("fixture")}})
	if e != nil {
		h.t.Fatal(e)
	}
	h.app = httptest.NewServer(handler)
	h.t.Cleanup(handler.(interface{ Close() }).Close)
	h.t.Cleanup(h.app.Close)
}
func TestNativeIndependentRefreshRestartAndRevocation(t *testing.T) {
	h := setup(t)
	h.login("alice")
	first := h.native("alice", "phone")
	second := h.native("alice", "tablet")
	code, _, web := h.request("POST", "/api/login", loginInput{"alice", "test-password"}, nil)
	if code != 200 {
		t.Fatal(code)
	}
	for _, c := range []credentials{first, second} {
		code, b, _ := h.vrequest("GET", "me", nil, c.Access, nil)
		if code != 200 || !bytes.Contains(b, []byte(c.Account)) {
			t.Fatalf("independent login: %d %s", code, b)
		}
	}
	h.request("GET", "/api/me", nil, web.Cookies()[0])
	key := "11111111-1111-4111-8111-111111111111"
	var results [2][]byte
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, b, _ := h.vrequest("POST", "auth/refresh", map[string]string{"refreshToken": first.Refresh}, "", map[string]string{"Idempotency-Key": key})
			if code != 200 {
				t.Errorf("refresh: %d %s", code, b)
			}
			results[i] = b
		}(i)
	}
	wg.Wait()
	if !bytes.Equal(results[0], results[1]) {
		t.Fatal("concurrent refresh replay differs")
	}
	rotated := decodeData[credentials](t, results[0])
	h.restart()
	code, replay, _ := h.vrequest("POST", "auth/refresh", map[string]string{"refreshToken": first.Refresh}, "", map[string]string{"Idempotency-Key": key})
	if code != 200 || !bytes.Equal(replay, results[0]) {
		t.Fatalf("restart/lost response replay: %d %s", code, replay)
	}
	code, b, _ := h.vrequest("DELETE", "auth/devices/"+second.Device, nil, rotated.Access, nil)
	if code != 204 {
		t.Fatalf("revoke: %d %s", code, b)
	}
	code, b, _ = h.vrequest("GET", "me", nil, second.Access, nil)
	if code != 401 || !bytes.Contains(b, []byte("session_revoked")) {
		t.Fatal("device not revoked")
	}
	code, _, _ = h.vrequest("GET", "me", nil, rotated.Access, nil)
	if code != 200 {
		t.Fatal("other device revoked")
	}
	code, b, _ = h.vrequest("GET", "auth/devices", nil, rotated.Access, nil)
	if code != 200 || bytes.Contains(b, []byte("Hash")) || bytes.Contains(b, []byte(rotated.Refresh)) {
		t.Fatal("device list leaked credentials")
	}
	code, _, _ = h.vrequest("POST", "auth/logout", nil, rotated.Access, nil)
	if code != 204 {
		t.Fatal(code)
	}
	code, _, _ = h.vrequest("POST", "auth/refresh", map[string]string{"refreshToken": first.Refresh}, "", map[string]string{"Idempotency-Key": key})
	if code != 401 {
		t.Fatal("revocation lost to replay")
	}
	raw, _ := os.ReadFile(filepath.Join(h.dir, "weazltunes.db"))
	wal, _ := os.ReadFile(filepath.Join(h.dir, "weazltunes.db-wal"))
	for _, secret := range []string{first.Access, first.Refresh, rotated.Refresh, "test-password"} {
		if bytes.Contains(raw, []byte(secret)) || bytes.Contains(wal, []byte(secret)) {
			t.Fatal("plaintext credential on disk")
		}
	}
}
func TestNativeMusicContractMutationsAndMedia(t *testing.T) {
	h := setup(t)
	h.login("alice")
	alice := h.native("alice", "phone")
	bob := h.native("bob", "phone")
	key := "22222222-2222-4222-8222-222222222222"
	code, b, _ := h.vrequest("GET", "library/tracks?limit=5", nil, alice.Access, nil)
	if code != 200 || len(decodeData[[]map[string]any](t, b)) != 5 {
		t.Fatalf("tracks: %d %s", code, b)
	}
	var paged struct {
		Cursor   string `json:"nextCursor"`
		Revision string `json:"snapshotRevision"`
	}
	_ = json.Unmarshal(b, &paged)
	code, b, _ = h.vrequest("GET", "library/tracks?limit=5&cursor="+paged.Cursor, nil, bob.Access, nil)
	if code != 409 || !bytes.Contains(b, []byte("snapshot_expired")) {
		t.Fatal("snapshot crossed account")
	}
	code, b, _ = h.vrequest("GET", "library/tracks?limit=5&cursor="+paged.Cursor, nil, alice.Access, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	for _, route := range []string{"library/albums", "library/albums/0", "library/artists", "library/artists/0", "library/tracks/0-0", "library/search?scope=track&query=Track", "library/search?scope=album&query=Tycho", "library/favorites"} {
		code, b, _ := h.vrequest("GET", route, nil, alice.Access, nil)
		if code != 200 {
			t.Fatalf("%s: %d %s", route, code, b)
		}
	}
	for _, kind := range []string{"track", "album", "artist"} {
		id := "0"
		if kind == "track" {
			id = "0-0"
		}
		code, b, _ := h.vrequest("PUT", "library/favorites/"+kind+"/"+id, map[string]bool{"enabled": true}, alice.Access, nil)
		if code != 200 {
			t.Fatalf("favorite: %d %s", code, b)
		}
	}
	body := map[string]any{"name": "Native fixture", "trackIds": []string{"0-0", "1-0", "0-0"}}
	code, b, head := h.vrequest("POST", "library/playlists", body, alice.Access, map[string]string{"Idempotency-Key": key})
	if code != 201 {
		t.Fatal(code, string(b))
	}
	playlist := decodeData[map[string]any](t, b)
	id := playlist["id"].(string)
	entries := playlist["entries"].([]any)
	if entries[0].(map[string]any)["entryId"] == entries[2].(map[string]any)["entryId"] {
		t.Fatal("duplicate entry collapsed")
	}
	code, replay, _ := h.vrequest("POST", "library/playlists", body, alice.Access, map[string]string{"Idempotency-Key": key})
	if code != 201 || !bytes.Equal(b, replay) {
		t.Fatal("create replay changed")
	}
	code, _, _ = h.vrequest("POST", "library/playlists", map[string]any{"name": "Different", "trackIds": []string{}}, alice.Access, map[string]string{"Idempotency-Key": key})
	if code != 409 {
		t.Fatal("key body conflict accepted")
	}
	code, _, _ = h.vrequest("PUT", "library/playlists/"+id, body, bob.Access, map[string]string{"If-Match": head.Get("ETag")})
	if code != 403 {
		t.Fatal("foreign playlist mutation")
	}
	body["trackIds"] = []string{"1-0", "0-0", "0-0"}
	code, b, head = h.vrequest("PUT", "library/playlists/"+id, body, alice.Access, map[string]string{"If-Match": head.Get("ETag")})
	if code != 200 {
		t.Fatal(code, string(b))
	}
	occurrence := map[string]any{"occurrenceId": "occurrence-1", "trackId": "0-0", "playedAt": "2026-10-03T20:00:00Z"}
	for i := 0; i < 2; i++ {
		code, b, _ := h.vrequest("POST", "library/scrobbles", occurrence, alice.Access, nil)
		if code != 200 {
			t.Fatal(code, string(b))
		}
	}
	n := 0
	for _, request := range h.nav.Requests {
		if request == "alice:scrobble" {
			n++
		}
	}
	if n != 1 {
		t.Fatal("scrobble repeated", n)
	}
	code, b, _ = h.vrequest("POST", "media/leases", map[string]string{"kind": "trackOriginal", "resourceId": "0-0"}, alice.Access, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	lease := decodeData[map[string]any](t, b)
	req, _ := http.NewRequest("GET", h.app.URL+lease["url"].(string), nil)
	req.Header.Set("Range", "bytes=0-43")
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || len(raw) != 44 || !bytes.HasPrefix(raw, []byte("RIFF")) {
		t.Fatal("leased original range failed")
	}
	if h.nav.Requests[len(h.nav.Requests)-1] != "alice:download" {
		t.Fatal("original used stream")
	}
	code, b, _ = h.vrequest("DELETE", "library/playlists/"+id, nil, alice.Access, map[string]string{"If-Match": head.Get("ETag")})
	if code != 204 {
		t.Fatal(code, string(b))
	}
}
