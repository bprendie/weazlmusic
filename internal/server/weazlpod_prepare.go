package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func isPod(r *http.Request) bool {
	values := r.Header.Values("X-Weazl-Client")
	return len(values) == 1 && strings.EqualFold(strings.TrimSpace(values[0]), "weazlpod")
}

// Authorization precedes cache lookup on every operation. Hashes are never credentials.
func (a *apiV1) podAuthorize(ctx context.Context, se *session, kind, id string) bool {
	if kind == "recordingSegment" {
		a.mu.Lock()
		defer a.mu.Unlock()
		var ar assetRecord
		var job recordingJob
		return a.get("asset", id, accountID(se.User), &ar) == nil && a.get("job", ar.Session, accountID(se.User), &job) == nil && !job.Tombstone && finalState(job.State)
	}
	if se.ServerURL == "" {
		return false
	}
	out, err := a.s.call(ctx, se, "getSong", url.Values{"id": {id}})
	return err == nil && len(out["song"]) > 0
}
func (a *apiV1) podPrepare(w http.ResponseWriter, r *http.Request, se *session, kind, id string) {
	if a.pod.encoder == "" {
		verror(w, 422, "unsupported", "MP3 encoder is unavailable.")
		return
	}
	if !a.podAuthorize(r.Context(), se, kind, id) {
		verror(w, 404, "not_found", "Audio source is unavailable to this account.")
		return
	}
	key := a.libraryID(se) + ":" + accountID(se.User) + ":" + kind + ":" + id
	a.mu.Lock()
	p := a.pod
	p.mu.Lock()
	if existing, ok := p.inflight[key]; ok {
		job := *p.jobs[existing]
		p.mu.Unlock()
		a.mu.Unlock()
		a.podStatus(w, r, se, job)
		return
	}
	for jobID, job := range p.jobs {
		if !a.now().Before(job.Expires) {
			delete(p.jobs, jobID)
		}
	}
	if p.running >= a.podWorkers() || len(p.jobs) >= 1000 {
		p.mu.Unlock()
		a.mu.Unlock()
		w.Header().Set("Retry-After", "2")
		verror(w, 429, "rate_limited", "Transcode workers are busy; retry shortly.")
		return
	}
	if !a.podReserve() {
		p.mu.Unlock()
		a.mu.Unlock()
		w.Header().Set("Retry-After", "30")
		verror(w, 507, "storage_full", "Insufficient derivative storage or recording reserve.")
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
	job := &podPreparation{ID: randomID(), State: "preparing", User: se.User, Library: a.libraryID(se), Kind: kind, Resource: id, Created: a.now(), Expires: a.now().Add(time.Hour), cancel: cancel}
	p.jobs[job.ID] = job
	p.inflight[key] = job.ID
	p.running++
	snapshot := *job
	// Register before releasing the admission lock so shutdown cannot miss a worker.
	a.wg.Add(1)
	go func() { defer a.wg.Done(); defer cancel(); a.podWork(ctx, se, job.ID, key) }()
	p.mu.Unlock()
	a.mu.Unlock()
	a.podStatus(w, r, se, snapshot)
}
func (a *apiV1) podWork(ctx context.Context, se *session, id, key string) {
	p := a.pod
	p.mu.Lock()
	job := *p.jobs[id]
	p.mu.Unlock()
	rep, code := a.podBuild(ctx, se, job)
	p.mu.Lock()
	defer p.mu.Unlock()
	j := p.jobs[id]
	if j.State != "cancelled" {
		if code != "" {
			j.State = "failed"
			j.Code = code
		} else {
			j.State = "ready"
			j.Representation = rep.ID
		}
	}
	delete(p.inflight, key)
	p.running--
	p.reserved.Add(-podReservation)
}
func (a *apiV1) podBuild(ctx context.Context, se *session, j podPreparation) (podRepresentation, string) {
	p := a.pod
	dir, err := os.MkdirTemp(p.root, "staging-")
	if err != nil {
		return podRepresentation{}, "storage_full"
	}
	defer os.RemoveAll(dir)
	source := filepath.Join(dir, "source")
	dst, err := os.OpenFile(source, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return podRepresentation{}, "storage_full"
	}
	defer dst.Close()
	var input io.ReadCloser
	if j.Kind == "recordingSegment" {
		a.mu.Lock()
		var ar assetRecord
		var job recordingJob
		err = a.get("asset", j.Resource, accountID(se.User), &ar)
		if err == nil {
			err = a.get("job", ar.Session, accountID(se.User), &job)
		}
		if err == nil && !job.Tombstone {
			input, err = os.Open(filepath.Join(a.s.cfg.DataDir, "recordings", ar.Path))
		} else {
			err = errors.New("removed recording")
		}
		a.mu.Unlock()
	} else {
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, "GET", a.s.endpoint(se, "download", url.Values{"id": {j.Resource}}), nil)
		if err == nil {
			var res *http.Response
			res, err = a.s.upstream.Do(req)
			if err == nil {
				input = res.Body
				if res.StatusCode != 200 {
					res.Body.Close()
					return podRepresentation{}, "upstream_rejected"
				}
			}
		}
	}
	if err != nil {
		return podRepresentation{}, "upstream_unavailable"
	}
	defer input.Close()
	// Copy in small blocks with reserve checks, instead of letting the encoder fetch a URL.
	guard := &podSourceWriter{a: a, w: dst, ctx: ctx}
	n, err := io.Copy(guard, io.LimitReader(input, podInputLimit+1))
	if guard.full {
		return podRepresentation{}, "storage_full"
	}
	if err != nil || n == 0 {
		return podRepresentation{}, "upstream_unavailable"
	}
	if n > podInputLimit {
		return podRepresentation{}, "unsupported"
	}
	if dst.Close() != nil {
		return podRepresentation{}, "storage_full"
	}
	_, hash, err := podHash(source)
	if err != nil {
		return podRepresentation{}, "storage_full"
	}
	rep, err := a.podPublish(ctx, se, source, hash)
	if err != nil {
		return rep, "transcode_failed"
	}
	return rep, ""
}

type podSourceWriter struct {
	a    *apiV1
	w    io.Writer
	ctx  context.Context
	n    int64
	full bool
}

func (g *podSourceWriter) Write(b []byte) (int, error) {
	if err := g.ctx.Err(); err != nil {
		return 0, err
	}
	if g.n == 0 || g.n%(1<<20) < int64(len(b)) {
		g.a.mu.Lock()
		s, err := g.a.storage()
		g.a.mu.Unlock()
		if err != nil || !diskReserveOK(g.a.s.cfg.DataDir, s.Reserve+s.Reserved) {
			g.full = true
			return 0, errors.New("disk reserve")
		}
	}
	n, err := g.w.Write(b)
	g.n += int64(n)
	return n, err
}
func (a *apiV1) podPreparationAPI(w http.ResponseWriter, r *http.Request, se *session) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/media/preparations/")
	p := a.pod
	p.mu.Lock()
	j, ok := p.jobs[id]
	if !ok || j.User != se.User || j.Library != a.libraryID(se) {
		p.mu.Unlock()
		verror(w, 404, "not_found", "Preparation not found or expired; create it again.")
		return
	}
	job := *j
	if !a.now().Before(job.Expires) {
		p.mu.Unlock()
		verror(w, 410, "asset_expired", "Preparation expired; create it again.")
		return
	}
	if r.Method == "DELETE" {
		if j.State == "preparing" {
			j.State = "cancelled"
			j.cancel()
		}
		p.mu.Unlock()
		w.WriteHeader(204)
		return
	}
	p.mu.Unlock()
	if r.Method != "GET" {
		verror(w, 405, "unsupported", "Use GET or DELETE.")
		return
	}
	if !a.podAuthorize(r.Context(), se, job.Kind, job.Resource) {
		verror(w, 404, "not_found", "Source is no longer available.")
		return
	}
	a.podStatus(w, r, se, job)
}
func (a *apiV1) podStatus(w http.ResponseWriter, r *http.Request, se *session, j podPreparation) {
	if j.State == "ready" {
		a.podLease(w, r, se, j)
		return
	}
	status := 200
	if j.State == "preparing" {
		status = 202
		w.Header().Set("Retry-After", "2")
	}
	w.Header().Set("Location", "/api/v1/media/preparations/"+j.ID)
	dataOut(w, status, j)
}
