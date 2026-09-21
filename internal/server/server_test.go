package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"weazltunes.local/web/internal/testnav"
)

type harness struct {
	app      *httptest.Server
	nav      *testnav.Server
	dir      string
	upstream string
	t        *testing.T
}

func setup(t *testing.T) *harness {
	t.Helper()
	nav := testnav.New()
	up := httptest.NewServer(nav)
	t.Cleanup(up.Close)
	dir := t.TempDir()
	handler, err := New(Config{DataDir: dir}, fstest.MapFS{"index.html": {Data: []byte("Weazl")}})
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(handler)
	t.Cleanup(app.Close)
	return &harness{app: app, nav: nav, dir: dir, upstream: up.URL, t: t}
}
func (h *harness) request(method, path string, body any, cookie *http.Cookie) (int, []byte, *http.Response) {
	h.t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, h.app.URL+path, bytes.NewReader(data))
	req.Header.Set("X-Weazl-Request", "1")
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b, res
}
func (h *harness) login(user string) *http.Cookie {
	h.t.Helper()
	code, b, res := h.request("POST", "/api/register", map[string]string{"username": "web-" + user, "password": "local-test-password"}, nil)
	if code != 200 {
		h.t.Fatalf("register: %d %s", code, b)
	}
	cookie := res.Cookies()[0]
	code, b, res = h.request("PUT", "/api/connection", map[string]string{"url": h.upstream, "username": user, "password": "test-password"}, cookie)
	if code != 200 {
		h.t.Fatalf("connection: %d %s", code, b)
	}
	c := res.Cookies()[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		h.t.Fatal("insecure cookie")
	}
	return c
}
func TestAuthenticationAndOrigin(t *testing.T) {
	h := setup(t)
	code, _, _ := h.request("GET", "/api/state", nil, nil)
	if code != 401 {
		t.Fatal(code)
	}
	code, _, _ = h.request("POST", "/api/login", map[string]string{"username": "alice", "password": "wrong"}, nil)
	if code != 401 {
		t.Fatal(code)
	}
	c := h.login("alice")
	req, _ := http.NewRequest("POST", h.app.URL+"/api/logout", strings.NewReader("{}"))
	req.AddCookie(c)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("missing mutation header accepted")
	}
	req, _ = http.NewRequest("POST", h.app.URL+"/api/logout", strings.NewReader("{}"))
	req.AddCookie(c)
	req.Header.Set("X-Weazl-Request", "1")
	req.Header.Set("Origin", "https://evil.example")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("cross-origin accepted")
	}
	code, _, _ = h.request("POST", "/api/logout", map[string]any{}, c)
	if code != 200 {
		t.Fatal(code)
	}
	code, _, _ = h.request("GET", "/api/me", nil, c)
	if code != 401 {
		t.Fatal("logout did not revoke session")
	}
}
func TestPlaylistWriteThroughAndOwnership(t *testing.T) {
	h := setup(t)
	alice := h.login("alice")
	bob := h.login("bob")
	code, b, _ := h.request("POST", "/api/playlists", map[string]any{"name": "Night shift", "songIds": []string{"0-0", "1-0"}}, alice)
	if code != 200 {
		t.Fatalf("create: %d %s", code, b)
	}
	if h.nav.Playlists["1"].Owner != "alice" || h.nav.Playlists["1"].SongCount != 2 {
		t.Fatal("playlist not written to signed-in user")
	}
	code, b, _ = h.request("GET", "/api/library/getPlaylists", nil, bob)
	if code != 200 || strings.Contains(string(b), "Night shift") {
		t.Fatal("playlist leaked to bob")
	}
	code, _, _ = h.request("POST", "/api/playlists", map[string]any{"id": "1", "name": "Stolen"}, bob)
	if code != 403 {
		t.Fatal("foreign playlist edit accepted")
	}
	code, _, _ = h.request("POST", "/api/playlist-delete", map[string]any{"id": "1"}, bob)
	if code != 403 {
		t.Fatal("foreign delete accepted")
	}
	code, _, _ = h.request("POST", "/api/playlists", map[string]any{"id": "1", "name": "After hours", "songIds": []string{"2-0"}, "remove": []int{0}}, alice)
	if code != 200 {
		t.Fatal(code)
	}
	p := h.nav.Playlists["1"]
	if p.Name != "After hours" || p.SongCount != 2 || p.Entry[0]["id"] != "1-0" {
		t.Fatal("update not written upstream")
	}
	code, _, _ = h.request("POST", "/api/playlist-delete", map[string]any{"id": "1"}, alice)
	if code != 200 || len(h.nav.Playlists) != 0 {
		t.Fatal("delete not written upstream")
	}
}
func TestEncryptedStateAndUserIsolation(t *testing.T) {
	h := setup(t)
	alice := h.login("alice")
	bob := h.login("bob")
	saved := userState{Stations: []station{{Name: "Private favorite", URL: "https://somafm.com/groovesalad.pls", Preset: true}}, Queue: []json.RawMessage{json.RawMessage(`{"id":"0-0","title":"Private track"}`)}}
	code, b, _ := h.request("PUT", "/api/state", saved, alice)
	if code != 200 {
		t.Fatalf("save: %d %s", code, b)
	}
	code, b, _ = h.request("GET", "/api/state", nil, bob)
	if code != 200 || strings.Contains(string(b), "Private") {
		t.Fatal("state leaked")
	}
	files, err := filepath.Glob(filepath.Join(h.dir, "*.bin"))
	if err != nil || len(files) != 4 {
		t.Fatal(files, err)
	}
	raw, _ := os.ReadFile(filepath.Join(h.dir, digest("state:web-alice:"+h.upstream+"/alice")+".bin"))
	if bytes.Contains(raw, []byte("Private")) {
		t.Fatal("state not encrypted")
	}
	st, err := newStore(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := st.read("state:web-alice:" + h.upstream + "/alice")
	if err != nil || len(restored.Stations) != 1 || restored.Stations[0].Name != "Private favorite" {
		t.Fatal("restart did not preserve settings", err)
	}
	rawKey, _ := os.ReadFile(filepath.Join(h.dir, "key"))
	if len(rawKey) != 32 || st == nil {
		t.Fatal("key invalid")
	}
	code, b, _ = h.request("GET", "/api/state", nil, alice)
	if code != 200 || !strings.Contains(string(b), "Private favorite") {
		t.Fatal("state not restored")
	}
}
func TestMediaRangeAndReadAllowlist(t *testing.T) {
	h := setup(t)
	c := h.login("alice")
	req, _ := http.NewRequest("GET", h.app.URL+"/api/media/stream?id=0-0", nil)
	req.AddCookie(c)
	req.Header.Set("Range", "bytes=0-43")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 206 || len(b) != 44 || !bytes.HasPrefix(b, []byte("RIFF")) {
		t.Fatalf("range broken: %d %d", res.StatusCode, len(b))
	}
	code, cover, _ := h.request("GET", "/api/media/cover?id=0", nil, c)
	if code != 200 || !bytes.Contains(cover, []byte("<svg")) {
		t.Fatal("cover art failed", code)
	}
	code, _, _ = h.request("GET", "/api/library/deletePlaylist?id=1", nil, c)
	if code != 404 {
		t.Fatal("write operation allowed through reads")
	}
}
func TestRadioURLPolicyAndPlaylistResolution(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "224.0.0.1", "2001:db8::1"} {
		if publicIP(net.ParseIP(ip)) {
			t.Fatalf("accepted %s", ip)
		}
	}
	if !publicIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public address rejected")
	}
	for _, raw := range []string{"file:///etc/passwd", "http://user:secret@example.com/x", "javascript:alert(1)"} {
		if validRadioURL(raw) == nil {
			t.Fatal(raw)
		}
	}
	for _, body := range []string{"[playlist]\nFile1=https://example.com/live.mp3\nNumberOfEntries=1", "#EXTM3U\nhttps://example.com/live.mp3"} {
		got, err := playlistTarget(body, "https://example.com/a.pls")
		if err != nil || got != "https://example.com/live.mp3" {
			t.Fatal(got, err)
		}
	}
	client := radioClient()
	client.Timeout = time.Second
	res, err := client.Get("http://127.0.0.1:4000/healthz")
	if err == nil {
		res.Body.Close()
		t.Fatal("SSRF accepted")
	}
}
