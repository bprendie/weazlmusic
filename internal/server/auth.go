package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"time"
)

type session struct {
	radio                                 *radioHub
	User, NavUser, ServerURL, Token, Salt string
	Admin                                 bool
	Expires                               time.Time
	ctx                                   context.Context
	cancel                                context.CancelFunc
}
type attempt struct {
	Count int
	Reset time.Time
}

func randomID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func (s *Server) allowLogin(w http.ResponseWriter, r *http.Request, user string) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.mu.Lock()
	now := time.Now()
	for k, a := range s.attempts {
		if now.After(a.Reset) {
			delete(s.attempts, k)
		}
	}
	limited := len(s.attempts) > 5000
	for _, key := range []string{"ip:" + host, "user:" + user} {
		a := s.attempts[key]
		if a.Reset.IsZero() {
			a.Reset = now.Add(time.Minute)
		}
		a.Count++
		s.attempts[key] = a
		if a.Count > 20 {
			limited = true
		}
	}
	s.mu.Unlock()
	if limited {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "Too many attempts. Try again in a minute.")
		return false
	}
	return true
}
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, a account, revokeAll bool) {
	se := &session{radio: newRadioHub(), User: a.Username, Admin: a.Admin, Expires: time.Now().Add(24 * time.Hour)}
	if c := a.Connection; c != nil {
		se.ServerURL = c.URL
		se.NavUser = c.Username
		se.Token = c.Token
		se.Salt = c.Salt
	}
	se.ctx, se.cancel = context.WithCancel(context.Background())
	id := randomID()
	s.mu.Lock()
	for k, v := range s.sessions {
		if time.Now().After(v.Expires) || (revokeAll && v.User == a.Username) {
			v.cancel()
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= 1000 {
		s.mu.Unlock()
		se.cancel()
		fail(w, 503, "Session capacity reached")
		return
	}
	if old, err := r.Cookie("weazl_session"); err == nil {
		key := digest(old.Value)
		if previous := s.sessions[key]; previous != nil {
			previous.cancel()
			delete(s.sessions, key)
		}
	}
	s.sessions[digest(id)] = se
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "weazl_session", Value: id, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	jsonOut(w, 200, identity(se))
}
func (s *Server) auth(next func(http.ResponseWriter, *http.Request, *session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("weazl_session")
		if err != nil {
			fail(w, 401, "Sign in to continue")
			return
		}
		key := digest(c.Value)
		s.mu.Lock()
		se := s.sessions[key]
		if se != nil && time.Now().After(se.Expires) {
			se.cancel()
			delete(s.sessions, key)
			se = nil
		}
		s.mu.Unlock()
		if se == nil {
			fail(w, 401, "Session expired. Sign in again.")
			return
		}
		next(w, r, se)
	}
}
func (s *Server) connected(next func(http.ResponseWriter, *http.Request, *session)) http.HandlerFunc {
	return s.auth(func(w http.ResponseWriter, r *http.Request, se *session) {
		if se.ServerURL == "" {
			fail(w, 409, "Configure your Navidrome connection first")
			return
		}
		next(w, r, se)
	})
}
func (s *Server) admin(next func(http.ResponseWriter, *http.Request, *session)) http.HandlerFunc {
	return s.auth(func(w http.ResponseWriter, r *http.Request, se *session) {
		if !se.Admin {
			fail(w, http.StatusForbidden, "Administrator access required")
			return
		}
		next(w, r, se)
	})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request, se *session) {
	c, _ := r.Cookie("weazl_session")
	s.mu.Lock()
	delete(s.sessions, digest(c.Value))
	se.cancel()
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "weazl_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.SecureCookie, SameSite: http.SameSiteStrictMode})
	jsonOut(w, 200, map[string]bool{"ok": true})
}
