package server

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) configureConnection(w http.ResponseWriter, r *http.Request, se *session) {
	var in struct {
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decode(r, &in) != nil || len(in.URL) > 2048 || len(in.Username) > 100 || len(in.Password) > 1024 || strings.TrimSpace(in.Username) == "" || in.Password == "" {
		fail(w, 400, "Enter the Navidrome server, username, and password")
		return
	}
	cleanURL, err := validNavidromeURL(in.URL)
	if err != nil {
		fail(w, 400, "Enter an http(s) server URL without credentials, query, or fragment")
		return
	}
	if !s.allowLogin(w, r, se.User) {
		return
	}
	c, err := s.authenticateNavidrome(r.Context(), cleanURL, strings.TrimSpace(in.Username), in.Password)
	if err != nil {
		fail(w, 422, "Could not connect. Check the server URL and Navidrome credentials.")
		return
	}
	candidate := *se
	candidate.ServerURL, candidate.NavUser, candidate.Salt, candidate.Token = c.URL, c.Username, c.Salt, c.Token
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	a, err := s.loadAccount(se.User)
	if err != nil {
		fail(w, 500, "Could not load your web-app account")
		return
	}
	candidate.NavUser = c.Username
	if err = s.store.migrateLegacyState(&candidate); err != nil {
		fail(w, 500, "Could not restore your previous saved settings")
		return
	}
	a.Connection = &c
	if err = s.store.writeRecord("account:"+a.Username, a); err != nil {
		fail(w, 500, "Could not save your connection")
		return
	}
	if err = s.migrateGlobalConfig(a); err != nil {
		fail(w, 500, "Could not migrate installation settings")
		return
	}
	s.startSession(w, r, a, true)
}

func (s *Server) authenticateNavidrome(ctx context.Context, serverURL, username, password string) (connection, error) {
	salt := randomID()
	h := md5.Sum([]byte(password + salt))
	c := connection{URL: serverURL, Username: username, Salt: salt, Token: hex.EncodeToString(h[:])}
	candidate := &session{ServerURL: c.URL, NavUser: c.Username, Salt: c.Salt, Token: c.Token}
	if _, err := s.call(ctx, candidate, "ping", nil); err != nil {
		return connection{}, err
	}
	if info, err := s.call(ctx, candidate, "getUser", url.Values{"username": {c.Username}}); err == nil {
		var user struct {
			Username string `json:"username"`
		}
		if json.Unmarshal(info["user"], &user) == nil && user.Username != "" {
			c.Username = user.Username
		}
	}
	return c, nil
}

func validNavidromeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid Navidrome URL")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
