package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"time"
)

type remoteEnrollment struct {
	ID      string
	Hash    string
	Epoch   string
	Status  int
	Body    []byte
	Expires time.Time
}

// Enrollment is idempotent independently of the generic API mutation lock.
func (a *apiV1) remoteEnrollmentHTTP(w http.ResponseWriter, r *http.Request, se *session) {
	key := r.Header.Get("Idempotency-Key")
	if se.DeviceID == "" || !uuidKey.MatchString(key) {
		verror(w, 422, "validation_failed", "Native device and UUID required.")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		verror(w, 422, "validation_failed", "Invalid body.")
		return
	}
	var normalized any
	if json.Unmarshal(raw, &normalized) != nil {
		verror(w, 422, "validation_failed", "Invalid JSON.")
		return
	}
	hash := remoteHash(normalized)
	id := digest(accountID(se.User) + se.DeviceID + r.URL.Path + key)
	a.remote.enrollmentMu.Lock()
	defer a.remote.enrollmentMu.Unlock()
	var old remoteEnrollment
	if a.get("remoteEnrollment", id, accountID(se.User), &old) == nil && a.now().Before(old.Expires) {
		if hash != old.Hash || old.Epoch != a.remote.epoch {
			verror(w, 409, "version_conflict", "Enrollment key belongs to another request or server epoch.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(old.Status)
		w.Write(old.Body)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	record := httptest.NewRecorder()
	a.remoteHTTP(record, r, se)
	if record.Code == 200 {
		old = remoteEnrollment{ID: id, Hash: hash, Epoch: a.remote.epoch, Status: record.Code, Body: record.Body.Bytes(), Expires: a.now().Add(24 * time.Hour)}
		if a.put("remoteEnrollment", id, accountID(se.User), old) != nil {
			verror(w, 503, "outcome_unknown", "Enrollment outcome could not be saved.")
			return
		}
	}
	for k, v := range record.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(record.Code)
	w.Write(record.Body.Bytes())
}
func (a *apiV1) remoteRevoke(id string) {
	if a.remote == nil {
		return
	}
	a.remote.mu.Lock()
	defer a.remote.mu.Unlock()
	if ch := a.remote.revoked[id]; ch != nil {
		close(ch)
		delete(a.remote.revoked, id)
	}
	if p := a.remote.players[id]; p != nil {
		p.attached = false
		p.Connection = ""
	}
}
