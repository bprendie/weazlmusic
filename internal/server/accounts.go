package server

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"regexp"
	"strings"
)

const passwordIterations = 600000

var accountName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

type connection struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Token    string `json:"token"`
	Salt     string `json:"salt"`
}
type account struct {
	LLM        *llmConfig  `json:"llm,omitempty"`
	Username   string      `json:"username"`
	Hash       string      `json:"hash"`
	Salt       string      `json:"salt"`
	Connection *connection `json:"connection,omitempty"`
	Admin      bool        `json:"admin,omitempty"`
}
type loginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) ensureDefaultAdmin() error {
	if _, err := s.loadAccount("weazladmin"); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	global, err := s.store.readGlobal()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err != nil {
		global = globalConfig{Version: 1}
	}
	salt := randomID()
	hash, err := derivePassword("admin", salt)
	if err != nil {
		return err
	}
	admin := account{Username: "weazladmin", Hash: hash, Salt: salt, Admin: true}
	if err = s.store.writeRecord("account:"+admin.Username, admin); err != nil {
		return err
	}
	if global.AdminUsername == "" {
		global.AdminUsername = admin.Username
	}
	return s.store.writeGlobal(global)
}

func derivePassword(password, salt string) (string, error) {
	b, e := pbkdf2.Key(sha256.New, password, []byte(salt), passwordIterations, 32)
	return hex.EncodeToString(b), e
}
func (s *Server) loadAccount(user string) (account, error) {
	var a account
	err := s.store.readRecord("account:"+user, &a)
	return a, err
}
func parseLogin(w http.ResponseWriter, r *http.Request) (loginInput, bool) {
	var in loginInput
	if decode(r, &in) != nil || len(in.Password) > 1024 || in.Password == "" {
		fail(w, 400, "Enter your web-app username and password")
		return in, false
	}
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	if !accountName.MatchString(in.Username) {
		fail(w, 400, "Use a username of 3–64 letters, numbers, dots, underscores, or hyphens")
		return in, false
	}
	return in, true
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	in, ok := parseLogin(w, r)
	if !ok {
		return
	}
	if !s.allowLogin(w, r, in.Username) {
		return
	}
	if len(in.Password) < 10 {
		fail(w, 400, "Use at least 10 characters for your web-app password")
		return
	}
	salt := randomID()
	hash, err := derivePassword(in.Password, salt)
	in.Password = ""
	if err != nil {
		fail(w, 500, "Could not create account")
		return
	}
	s.accountsMu.Lock()
	_, err = s.loadAccount(in.Username)
	if err == nil {
		s.accountsMu.Unlock()
		fail(w, 409, "That web-app username is already registered")
		return
	}
	if !os.IsNotExist(err) {
		s.accountsMu.Unlock()
		fail(w, 500, "Could not read accounts")
		return
	}
	a := account{Username: in.Username, Hash: hash, Salt: salt}
	global, globalErr := s.store.readGlobal()
	if os.IsNotExist(globalErr) || (globalErr == nil && global.AdminUsername == "") {
		a.Admin = true
		if globalErr != nil {
			global = globalConfig{Version: 1}
		}
		global.AdminUsername = a.Username
		if err = s.store.writeGlobal(global); err != nil {
			s.accountsMu.Unlock()
			fail(w, 500, "Could not initialize admin settings")
			return
		}
	} else if globalErr != nil {
		s.accountsMu.Unlock()
		fail(w, 500, "Could not read installation settings")
		return
	}
	err = s.store.writeRecord("account:"+a.Username, a)
	s.accountsMu.Unlock()
	if err != nil {
		fail(w, 500, "Could not save account")
		return
	}
	s.startSession(w, r, a, false)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	in, ok := parseLogin(w, r)
	if !ok {
		return
	}
	if !s.allowLogin(w, r, in.Username) {
		return
	}
	a, accountErr := s.loadAccount(in.Username)
	if accountErr == nil && a.Admin {
		salt := a.Salt
		hash, hashErr := derivePassword(in.Password, salt)
		in.Password = ""
		if hashErr != nil || subtle.ConstantTimeCompare([]byte(hash), []byte(a.Hash)) != 1 {
			fail(w, 401, "Incorrect local admin password")
			return
		}
		s.startSession(w, r, a, false)
		return
	}
	cfg, err := s.store.readGlobal()
	if err != nil || cfg.NavidromeURL == "" {
		fail(w, 409, "The administrator must configure the Navidrome server first")
		return
	}
	password := in.Password
	c, navErr := s.authenticateNavidrome(r.Context(), cfg.NavidromeURL, in.Username, password)
	in.Password = ""
	if navErr != nil {
		// Existing local accounts can finish their migration if Navidrome is
		// unavailable; newly created users never get a second password.
		if accountErr == nil && a.Hash != "" {
			hash, hashErr := derivePassword(password, a.Salt)
			if hashErr == nil && subtle.ConstantTimeCompare([]byte(hash), []byte(a.Hash)) == 1 {
				s.startSession(w, r, a, false)
				return
			}
		}
		fail(w, 401, "Navidrome rejected those credentials")
		return
	}
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	if accountErr != nil && !os.IsNotExist(accountErr) {
		fail(w, 500, "Could not load account")
		return
	}
	if os.IsNotExist(accountErr) {
		a = account{Username: in.Username}
	}
	a.Connection = &c
	if err = s.store.migrateLegacyState(&session{User: a.Username, ServerURL: c.URL, NavUser: c.Username}); err != nil {
		fail(w, 500, "Could not restore your previous saved settings")
		return
	}
	if err = s.store.writeRecord("account:"+a.Username, a); err != nil {
		fail(w, 500, "Could not save account")
		return
	}
	s.startSession(w, r, a, true)
}
func identity(se *session) map[string]any {
	out := map[string]any{"username": se.User, "admin": se.Admin, "connection": nil}
	if se.ServerURL != "" {
		out["connection"] = map[string]string{"url": se.ServerURL, "username": se.NavUser}
	}
	return out
}
