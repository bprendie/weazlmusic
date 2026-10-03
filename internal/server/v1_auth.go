package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

type deviceSession struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	ClientID       string    `json:"clientId"`
	User           string    `json:"-"`
	Username       string    `json:"username"`
	Library        string    `json:"libraryId"`
	RefreshHash    string    `json:"refreshHash"`
	AccessHash     string    `json:"accessHash"`
	AccessExpires  time.Time `json:"accessExpiresAt"`
	RefreshExpires time.Time `json:"refreshExpiresAt"`
	LastSeen       time.Time `json:"lastSeen"`
	Revoked        bool      `json:"revoked"`
}
type credentials struct {
	Access         string    `json:"accessToken"`
	AccessExpires  time.Time `json:"accessExpiresAt"`
	Refresh        string    `json:"refreshToken"`
	RefreshExpires time.Time `json:"refreshExpiresAt"`
	Device         string    `json:"deviceId"`
	Account        string    `json:"accountId"`
	Library        string    `json:"libraryId"`
}
type refreshReplay struct {
	Device  string
	Hash    string
	Key     string
	Result  credentials
	Expires time.Time
}

func (a *apiV1) issue(d *deviceSession) credentials {
	now := a.now()
	d.AccessExpires = now.Add(15 * time.Minute)
	d.RefreshExpires = now.Add(30 * 24 * time.Hour)
	d.LastSeen = now
	c := credentials{randomID(), d.AccessExpires, randomID(), d.RefreshExpires, d.ID, accountID(d.Username), d.Library}
	d.AccessHash = digest(c.Access)
	d.RefreshHash = digest(c.Refresh)
	return c
}
func (a *apiV1) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		DeviceName string `json:"deviceName"`
		ClientID   string `json:"clientId"`
	}
	if decode(r, &in) != nil || len(in.DeviceName) > 100 || strings.TrimSpace(in.DeviceName) == "" || len(in.ClientID) > 100 || in.ClientID == "" {
		verror(w, 422, "validation_failed", "Enter credentials, deviceName and clientId.")
		return
	}
	b, _ := json.Marshal(loginInput{in.Username, in.Password})
	in.Password = ""
	req := r.Clone(r.Context())
	req.Body = httpBody(b)
	req.Header = req.Header.Clone()
	req.Header.Del("Cookie")
	rw := httptest.NewRecorder()
	a.s.login(rw, req)
	if rw.Code != 200 {
		code := "unauthorized"
		if rw.Code == 429 {
			code = "rate_limited"
		} else if rw.Code >= 500 {
			code = "upstream_unavailable"
		}
		verror(w, rw.Code, code, "Sign-in failed. Check credentials and the server connection.")
		return
	}
	cookie := rw.Result().Cookies()[0]
	a.s.mu.Lock()
	se := a.s.sessions[digest(cookie.Value)]
	delete(a.s.sessions, digest(cookie.Value))
	a.s.mu.Unlock()
	defer se.cancel()
	a.mu.Lock()
	defer a.mu.Unlock()
	d := deviceSession{ID: randomID(), Name: in.DeviceName, ClientID: in.ClientID, Username: se.User, Library: a.libraryID(se)}
	c := a.issue(&d)
	if a.put("device", d.ID, accountID(d.Username), d) != nil {
		verror(w, 503, "storage_full", "Could not save device session.")
		return
	}
	dataOut(w, 200, c)
}
func httpBody(b []byte) *bodyReader { return &bodyReader{bytes.NewReader(b)} }

type bodyReader struct{ *bytes.Reader }

func (*bodyReader) Close() error { return nil }
func (a *apiV1) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"refreshToken"`
	}
	key := r.Header.Get("Idempotency-Key")
	if decode(r, &in) != nil || !uuidKey.MatchString(key) || len(in.Token) != 64 {
		verror(w, 422, "validation_failed", "Refresh token and UUID idempotency key required.")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	hash := digest(in.Token)
	var rr refreshReplay
	if a.get("refresh", hash, "*", &rr) == nil {
		var d deviceSession
		if a.get("device", rr.Device, "*", &d) == nil && !d.Revoked && a.deviceLibraryValid(d) && rr.Key == key && a.now().Before(rr.Expires) {
			dataOut(w, 200, rr.Result)
			return
		}
		verror(w, 401, "session_revoked", "Refresh credential is no longer valid.")
		return
	}
	rows, e := a.list("device", "*")
	if e != nil {
		verror(w, 503, "storage_full", "Device store unavailable.")
		return
	}
	for _, row := range rows {
		var d deviceSession
		_ = json.Unmarshal(row, &d)
		if d.RefreshHash != hash {
			continue
		}
		if d.Revoked || !a.now().Before(d.RefreshExpires) || !a.deviceLibraryValid(d) {
			break
		}
		c := a.issue(&d)
		rr = refreshReplay{d.ID, hash, key, c, a.now().Add(2 * time.Minute)}
		// Device rotation and the exact encrypted retry response commit together.
		dbb, _ := a.seal(d)
		rbb, _ := a.seal(rr)
		tx, e := a.db.Begin()
		if e == nil {
			_, e = tx.Exec("UPDATE records SET body=? WHERE kind='device' AND id=?", dbb, d.ID)
			if e == nil {
				_, e = tx.Exec("INSERT INTO records VALUES('refresh',?,?,?)", hash, accountID(d.Username), rbb)
			}
			if e == nil {
				e = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
		}
		if e != nil {
			verror(w, 503, "storage_full", "Could not rotate credential.")
			return
		}
		dataOut(w, 200, c)
		return
	}
	verror(w, 401, "session_revoked", "Refresh credential is no longer valid.")
}
func (a *apiV1) auth(next func(http.ResponseWriter, *http.Request, *session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == r.Header.Get("Authorization") || token == "" {
			rw := httptest.NewRecorder()
			called := false
			a.s.auth(func(_ http.ResponseWriter, r *http.Request, se *session) { called = true; next(w, r, se) })(rw, r)
			if !called {
				verror(w, 401, "unauthorized", "Sign in to continue.")
			}
			return
		}
		hash := digest(token)
		a.mu.Lock()
		rows, e := a.list("device", "*")
		a.mu.Unlock()
		if e != nil {
			verror(w, 503, "storage_full", "Device store unavailable.")
			return
		}
		for _, row := range rows {
			var d deviceSession
			_ = json.Unmarshal(row, &d)
			if d.AccessHash != hash {
				continue
			}
			if d.Revoked {
				verror(w, 401, "session_revoked", "Device session was revoked.")
				return
			}
			if !a.now().Before(d.AccessExpires) {
				break
			}
			se, e := a.deviceIdentity(d)
			if e != nil || a.libraryID(se) != d.Library {
				verror(w, 401, "session_revoked", "Account connection changed. Sign in again.")
				return
			}
			se.DeviceID = d.ID
			next(w, r, se)
			return
		}
		verror(w, 401, "unauthorized", "Access credential expired or invalid.")
	}
}
func (a *apiV1) deviceIdentity(d deviceSession) (*session, error) {
	acc, e := a.s.loadAccount(d.Username)
	if e != nil {
		return nil, e
	}
	se := &session{User: acc.Username, Admin: acc.Admin, Expires: d.AccessExpires, ctx: context.Background(), cancel: func() {}, radio: newRadioHub()}
	if c := acc.Connection; c != nil {
		se.ServerURL, se.NavUser, se.Token, se.Salt = c.URL, c.Username, c.Token, c.Salt
	}
	return se, nil
}
func (a *apiV1) devices(w http.ResponseWriter, r *http.Request, se *session) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/auth/")
	if path == "devices" && r.Method == "GET" {
		a.mu.Lock()
		rows, e := a.list("device", accountID(se.User))
		a.mu.Unlock()
		if e != nil {
			verror(w, 503, "storage_full", "Device store unavailable.")
			return
		}
		out := []any{}
		for _, row := range rows {
			var d deviceSession
			_ = json.Unmarshal(row, &d)
			if !d.Revoked {
				out = append(out, map[string]any{"id": d.ID, "name": d.Name, "lastSeen": d.LastSeen, "current": d.ID == se.DeviceID})
			}
		}
		dataOut(w, 200, out)
		return
	}
	id := ""
	if path == "logout" && r.Method == "POST" {
		id = se.DeviceID
	} else if strings.HasPrefix(path, "devices/") && r.Method == "DELETE" {
		id = strings.TrimPrefix(path, "devices/")
	}
	if id == "" {
		verror(w, 422, "validation_failed", "A native device session is required.")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var d deviceSession
	if a.get("device", id, accountID(se.User), &d) != nil {
		verror(w, 404, "not_found", "Device not found.")
		return
	}
	d.Revoked = true
	a.cancelDeviceMood(d.ID)
	if a.put("device", id, accountID(se.User), d) != nil {
		verror(w, 503, "storage_full", "Could not revoke device.")
		return
	}
	w.WriteHeader(204)
}
func (a *apiV1) revokeDevices(user string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows, e := a.list("device", accountID(user))
	if e != nil {
		return
	}
	for _, row := range rows {
		var d deviceSession
		_ = json.Unmarshal(row, &d)
		d.Revoked = true
		a.cancelDeviceMood(d.ID)
		_ = a.put("device", d.ID, accountID(user), d)
	}
}

func (a *apiV1) deviceLibraryValid(d deviceSession) bool {
	se, err := a.deviceIdentity(d)
	return err == nil && a.libraryID(se) == d.Library
}
