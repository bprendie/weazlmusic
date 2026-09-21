package server

import (
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
	salt := randomID()
	h := md5.Sum([]byte(in.Password + salt))
	in.Password = ""
	c := connection{URL: cleanURL, Username: strings.TrimSpace(in.Username), Salt: salt, Token: hex.EncodeToString(h[:])}
	candidate := *se
	candidate.ServerURL = c.URL
	candidate.NavUser = c.Username
	candidate.Salt = c.Salt
	candidate.Token = c.Token
	if _, err = s.call(r.Context(), &candidate, "ping", nil); err != nil {
		fail(w, 422, "Could not connect. Check the server URL and Navidrome credentials.")
		return
	}
	// Use the canonical upstream identity for playlist ownership checks.
	if info, e := s.call(r.Context(), &candidate, "getUser", url.Values{"username": {c.Username}}); e == nil {
		var user struct {
			Username string `json:"username"`
		}
		if json.Unmarshal(info["user"], &user) == nil && user.Username != "" {
			c.Username = user.Username
		}
	}
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

func validNavidromeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid Navidrome URL")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
