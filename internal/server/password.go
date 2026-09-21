package server

import (
	"crypto/subtle"
	"net/http"
)

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, se *session) {
	var in struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if decode(r, &in) != nil || in.Current == "" || len(in.Next) < 10 || len(in.Next) > 1024 {
		fail(w, 400, "Use a new password of at least 10 characters")
		return
	}
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	a, err := s.loadAccount(se.User)
	if err != nil {
		fail(w, 500, "Could not load account")
		return
	}
	old, err := derivePassword(in.Current, a.Salt)
	if err != nil || subtle.ConstantTimeCompare([]byte(old), []byte(a.Hash)) != 1 {
		fail(w, 401, "Current password is incorrect")
		return
	}
	salt := randomID()
	hash, err := derivePassword(in.Next, salt)
	if err != nil {
		fail(w, 500, "Could not update password")
		return
	}
	a.Hash, a.Salt = hash, salt
	if err = s.store.writeRecord("account:"+a.Username, a); err != nil {
		fail(w, 500, "Could not save password")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
