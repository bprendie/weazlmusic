package server

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
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
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		fail(w, 400, "Enter an http(s) server URL without credentials, query, or fragment")
		return
	}
	if !s.allowLogin(w, r, se.User) {
		return
	}
	salt := randomID()
	h := md5.Sum([]byte(in.Password + salt))
	in.Password = ""
	c := connection{URL: strings.TrimRight(u.String(), "/"), Username: strings.TrimSpace(in.Username), Salt: salt, Token: hex.EncodeToString(h[:])}
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
	s.startSession(w, r, a, true)
}
