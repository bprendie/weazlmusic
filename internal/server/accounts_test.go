package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"weazltunes.local/web/internal/testnav"
)

func TestLocalAccountAndSavedConnection(t *testing.T) {
	h := setup(t)
	code, b, res := h.request("POST", "/api/register", map[string]string{"username": "listener", "password": "local-password"}, nil)
	if code != 200 || len(h.nav.Requests) != 0 || !strings.Contains(string(b), `"connection":null`) {
		t.Fatalf("local registration contacted Navidrome: %d %s", code, b)
	}
	cookie := res.Cookies()[0]
	code, _, _ = h.request("GET", "/api/library/getPlaylists", nil, cookie)
	if code != 409 {
		t.Fatal("unconfigured library allowed", code)
	}
	code, _, _ = h.request("POST", "/api/login", map[string]string{"username": "listener", "password": "test-password"}, nil)
	if code != 401 {
		t.Fatal("Navidrome password accepted as local password")
	}
	code, _, _ = h.request("POST", "/api/register", map[string]string{"username": "listener", "password": "another-password"}, nil)
	if code != 409 {
		t.Fatal("account overwritten")
	}
	code, b, res = h.request("PUT", "/api/connection", map[string]string{"url": h.upstream, "username": "alice", "password": "test-password"}, cookie)
	if code != 200 || strings.Contains(string(b), "token") || strings.Contains(string(b), "salt") {
		t.Fatalf("connection response invalid: %d %s", code, b)
	}
	nextCookie := res.Cookies()[0]
	code, _, _ = h.request("GET", "/api/me", nil, cookie)
	if code != 401 {
		t.Fatal("old session not revoked")
	}
	code, _, _ = h.request("PUT", "/api/connection", map[string]string{"url": h.upstream, "username": "alice", "password": "bad-password"}, nextCookie)
	if code != 422 {
		t.Fatal("bad credentials saved")
	}
	code, b, _ = h.request("GET", "/api/me", nil, nextCookie)
	if code != 200 || !strings.Contains(string(b), h.upstream) {
		t.Fatal("failed update lost valid connection")
	}
	// A new application instance must restore both the local account and its connection.
	handler, err := New(Config{DataDir: h.dir}, fstest.MapFS{"index.html": {Data: []byte("Weazl")}})
	if err != nil {
		t.Fatal(err)
	}
	restarted := httptest.NewServer(handler)
	defer restarted.Close()
	old := h.app
	h.app = restarted
	defer func() { h.app = old }()
	before := len(h.nav.Requests)
	code, b, res = h.request("POST", "/api/login", map[string]string{"username": "LISTENER", "password": "local-password"}, nil)
	if code != 200 || len(h.nav.Requests) != before {
		t.Fatalf("local login depended on upstream: %d %s", code, b)
	}
	var me struct {
		Username   string `json:"username"`
		Connection struct {
			URL      string `json:"url"`
			Username string `json:"username"`
		} `json:"connection"`
	}
	if json.Unmarshal(b, &me) != nil || me.Username != "listener" || me.Connection.Username != "alice" || me.Connection.URL != h.upstream {
		t.Fatal("saved connection not restored")
	}
	code, _, _ = h.request("POST", "/api/playlists", map[string]any{"name": "Local user mix", "songIds": []string{"0-0"}}, res.Cookies()[0])
	if code != 200 || h.nav.Playlists["1"].Owner != "alice" {
		t.Fatal("playlist owner is not configured Navidrome user")
	}
}
func TestUsersChooseDifferentServers(t *testing.T) {
	h := setup(t)
	other := testnav.New()
	otherServer := httptest.NewServer(other)
	defer otherServer.Close()
	for i, user := range []string{"first-user", "second-user"} {
		target := h.upstream
		if i == 1 {
			target = otherServer.URL
		}
		code, _, res := h.request("POST", "/api/register", map[string]string{"username": user, "password": "local-password"}, nil)
		if code != 200 {
			t.Fatal(code)
		}
		code, _, res = h.request("PUT", "/api/connection", map[string]string{"url": target, "username": "alice", "password": "test-password"}, res.Cookies()[0])
		if code != 200 {
			t.Fatal(code)
		}
		code, _, _ = h.request("POST", "/api/playlists", map[string]any{"name": user, "songIds": []string{"0-0"}}, res.Cookies()[0])
		if code != http.StatusOK {
			t.Fatal(code)
		}
	}
	if h.nav.Playlists["1"].Name != "first-user" || other.Playlists["1"].Name != "second-user" {
		t.Fatal("users shared a hardcoded server")
	}
}

func TestGlobalConfigMigratesOnceAndStaysEncrypted(t *testing.T) {
	h := setup(t)
	cookie := h.login("alice")
	_ = cookie
	st, err := newStore(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := st.readGlobal()
	if err != nil || cfg.Version != 1 || cfg.NavidromeURL != h.upstream {
		t.Fatalf("global config not seeded: %#v %v", cfg, err)
	}
	raw, err := os.ReadFile(filepath.Join(h.dir, digest("global:config")+".bin"))
	if err != nil || bytes.Contains(raw, []byte(h.upstream)) {
		t.Fatal("global config was not encrypted")
	}
	second := account{Username: "later", Connection: &connection{URL: "https://different.invalid"}}
	if err = (&Server{store: st}).migrateGlobalConfig(second); err != nil {
		t.Fatal(err)
	}
	cfg, err = st.readGlobal()
	if err != nil || cfg.NavidromeURL != h.upstream {
		t.Fatal("global config was overwritten")
	}
}

func TestAdminInstallationSettingsAreProtected(t *testing.T) {
	h := setup(t)
	admin := h.login("alice")
	user := h.login("bob")
	code, body, _ := h.request("GET", "/api/admin/config", nil, admin)
	if code != http.StatusOK || !strings.Contains(string(body), h.upstream) {
		t.Fatalf("admin config unavailable: %d %s", code, body)
	}
	code, _, _ = h.request("GET", "/api/admin/config", nil, user)
	if code != http.StatusForbidden {
		t.Fatalf("non-admin config access accepted: %d", code)
	}
	code, _, _ = h.request("PUT", "/api/admin/config", map[string]any{"navidromeURL": h.upstream, "provider": "off"}, user)
	if code != http.StatusForbidden {
		t.Fatalf("non-admin config write accepted: %d", code)
	}
	code, _, _ = h.request("PUT", "/api/admin/config", map[string]any{"navidromeURL": h.upstream, "provider": "off"}, admin)
	if code != http.StatusOK {
		t.Fatalf("admin config write failed: %d", code)
	}
}

func TestLegacySettingsMigrateAfterVerifiedConnection(t *testing.T) {
	h := setup(t)
	st, err := newStore(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy := userState{Stations: []station{{Name: "My existing station", URL: "https://example.com/live.mp3", Preset: true}}, Queue: []json.RawMessage{json.RawMessage(`{"id":"0-0","title":"Saved track"}`)}}
	if err = st.write(h.upstream+"/alice", legacy); err != nil {
		t.Fatal(err)
	}
	code, _, res := h.request("POST", "/api/register", map[string]string{"username": "new-local-account", "password": "local-password"}, nil)
	if code != 200 {
		t.Fatal(code)
	}
	cookie := res.Cookies()[0]
	code, _, _ = h.request("PUT", "/api/connection", map[string]string{"url": h.upstream, "username": "alice", "password": "incorrect"}, cookie)
	if code != 422 {
		t.Fatal(code)
	}
	code, b, _ := h.request("GET", "/api/state", nil, cookie)
	if code != 200 || strings.Contains(string(b), "My existing station") {
		t.Fatal("legacy settings exposed without valid upstream credentials")
	}
	code, _, res = h.request("PUT", "/api/connection", map[string]string{"url": h.upstream, "username": "alice", "password": "test-password"}, cookie)
	if code != 200 {
		t.Fatal(code)
	}
	code, b, _ = h.request("GET", "/api/state", nil, res.Cookies()[0])
	if code != 200 || !strings.Contains(string(b), "My existing station") {
		t.Fatal("legacy state was lost")
	}
	original, err := st.read(h.upstream + "/alice")
	if err != nil || len(original.Queue) != 1 {
		t.Fatal("legacy state was modified", err)
	}
}
