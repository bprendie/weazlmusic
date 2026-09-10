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
}
type loginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
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
	a, err := s.loadAccount(in.Username)
	salt := a.Salt
	if err != nil {
		salt = "unknown-account-timing-salt"
	}
	hash, hashErr := derivePassword(in.Password, salt)
	in.Password = ""
	if err != nil || hashErr != nil || subtle.ConstantTimeCompare([]byte(hash), []byte(a.Hash)) != 1 {
		fail(w, 401, "Incorrect web-app username or password")
		return
	}
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	a, err = s.loadAccount(in.Username)
	if err != nil {
		fail(w, 500, "Could not load account")
		return
	}
	s.startSession(w, r, a, false)
}
func identity(se *session) map[string]any {
	out := map[string]any{"username": se.User, "connection": nil}
	if se.ServerURL != "" {
		out["connection"] = map[string]string{"url": se.ServerURL, "username": se.NavUser}
	}
	return out
}
