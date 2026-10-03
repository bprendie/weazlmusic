package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Details   any    `json:"details"`
}

func verror(w http.ResponseWriter, status int, code, msg string) {
	jsonOut(w, status, map[string]any{"error": apiError{code, msg, (status >= 500 || status == 429) && code != "outcome_unknown", map[string]any{}}})
}
func dataOut(w http.ResponseWriter, status int, v any) { jsonOut(w, status, map[string]any{"data": v}) }
func etag(version int64) string                        { return `"v` + itoa(version) + `"` }
func precondition(w http.ResponseWriter, r *http.Request, v int64) bool {
	if r.Header.Get("If-Match") == "" {
		verror(w, 428, "validation_failed", "Refresh this item before editing.")
		return false
	}
	if r.Header.Get("If-Match") != etag(v) {
		verror(w, 409, "version_conflict", "This item changed. Refresh and try again.")
		return false
	}
	return true
}

var uuidKey = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type replay struct {
	Hash    string
	Status  int
	Headers http.Header
	Body    []byte
	Expires time.Time
}
type bufferedResponse struct {
	head   http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.head }
func (b *bufferedResponse) WriteHeader(s int) {
	if b.status == 0 {
		b.status = s
	}
}
func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = 200
	}
	return b.body.Write(p)
}
func (a *apiV1) mutation(w http.ResponseWriter, r *http.Request, se *session, next func(http.ResponseWriter, *http.Request, *session)) {
	key := r.Header.Get("Idempotency-Key")
	if !uuidKey.MatchString(key) {
		verror(w, 422, "validation_failed", "Idempotency-Key must be a UUID.")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		verror(w, 422, "validation_failed", "Invalid request body.")
		return
	}
	var normalized any
	if len(raw) > 0 && json.Unmarshal(raw, &normalized) != nil {
		verror(w, 422, "validation_failed", "Invalid JSON.")
		return
	}
	norm, _ := json.Marshal(normalized)
	id := digest(accountID(se.User) + a.libraryID(se) + r.Method + r.URL.Path + key)
	hash := digest(string(norm) + r.Header.Get("If-Match"))
	a.mu.Lock()
	defer a.mu.Unlock()
	var old replay
	if a.get("mutation", id, accountID(se.User), &old) == nil && a.now().Before(old.Expires) {
		if old.Hash != hash {
			verror(w, 409, "version_conflict", "Idempotency key was used for another request.")
			return
		}
		writeReplay(w, old)
		return
	}
	// Persist an ambiguous marker before contacting any upstream. A crash never repeats a write blindly.
	pending := replay{Hash: hash, Status: 409, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"error":{"code":"outcome_unknown","message":"The previous request was interrupted; inspect its result before retrying.","retryable":false,"details":{}}}`), Expires: a.now().Add(30 * 24 * time.Hour)}
	if err = a.put("mutation", id, accountID(se.User), pending); err != nil {
		verror(w, 503, "storage_full", "Could not durably record this operation.")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	b := &bufferedResponse{head: http.Header{}}
	next(b, r, se)
	if b.status == 0 {
		b.status = 204
	}
	result := replay{Hash: hash, Status: b.status, Headers: b.head, Body: b.body.Bytes(), Expires: a.now().Add(30 * 24 * time.Hour)}
	if strings.Contains(r.URL.Path, "flight-recorder") {
		result.Expires = a.now().Add(100 * 365 * 24 * time.Hour)
	}
	if err = a.put("mutation", id, accountID(se.User), result); err != nil {
		verror(w, 503, "outcome_unknown", "Operation finished but its response could not be saved.")
		return
	}
	writeReplay(w, result)
}
func writeReplay(w http.ResponseWriter, v replay) {
	for k, values := range v.Headers {
		w.Header()[k] = values
	}
	w.WriteHeader(v.Status)
	_, _ = w.Write(v.Body)
}
func (a *apiV1) routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/info", func(w http.ResponseWriter, r *http.Request) {
		dataOut(w, 200, map[string]any{"installationId": a.installation, "apiVersion": 1, "contractRevision": contractRevision, "minimumClientVersion": "1.0"})
	})
	m.HandleFunc("POST /api/v1/auth/login", a.login)
	m.HandleFunc("POST /api/v1/auth/refresh", a.refresh)
	handler := a.auth(func(w http.ResponseWriter, r *http.Request, se *session) {
		p := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		if r.Method != "GET" && r.Method != "HEAD" && p != "media/leases" && p != "auth/logout" && !strings.HasPrefix(p, "auth/devices/") {
			a.mutation(w, r, se, a.dispatch)
			return
		}
		a.dispatch(w, r, se)
	})
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		m.HandleFunc(method+" /api/v1/", handler)
	}
}
func (a *apiV1) dispatch(w http.ResponseWriter, r *http.Request, se *session) {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	switch {
	case p == "me" && r.Method == "GET":
		dataOut(w, 200, map[string]any{"accountId": accountID(se.User), "username": se.User, "libraryId": a.libraryID(se), "libraryAvailable": se.ServerURL != "", "roles": func() []string {
			if se.Admin {
				return []string{"admin"}
			}
			return []string{"user"}
		}()})
	case p == "capabilities" && r.Method == "GET":
		a.capabilities(w, se)
	case strings.HasPrefix(p, "auth/"):
		a.devices(w, r, se)
	case strings.HasPrefix(p, "library/"):
		a.library(w, r, se)
	case p == "media/leases":
		a.lease(w, r, se)
	case strings.HasPrefix(p, "radio/"):
		a.stations(w, r, se)
	case strings.HasPrefix(p, "flight-recorder/"):
		a.recorder(w, r, se)
	case strings.HasPrefix(p, "mood/") || strings.HasPrefix(p, "preferences/curator"):
		a.moodAPI(w, r, se)
	case p == "queue":
		a.queueAPI(w, r, se)
	default:
		verror(w, 404, "not_found", "Unknown v1 endpoint.")
	}
}
