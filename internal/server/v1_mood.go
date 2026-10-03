package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type nativeMoodEvent struct {
	Sequence int            `json:"sequence"`
	Type     string         `json:"type"`
	Playlist *moodPlaylist  `json:"playlist,omitempty"`
	Track    map[string]any `json:"track,omitempty"`
	Count    int            `json:"count"`
	Target   int            `json:"target"`
	Message  string         `json:"message,omitempty"`
}
type nativeMood struct {
	ID       string            `json:"jobId"`
	State    string            `json:"state"`
	Username string            `json:"username"`
	Device   string            `json:"deviceId"`
	Events   []nativeMoodEvent `json:"events"`
}

func (a *apiV1) moodAPI(w http.ResponseWriter, r *http.Request, se *session) {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if strings.HasPrefix(p, "preferences/curator") {
		rw := httptestResponse()
		switch {
		case p == "preferences/curator" && r.Method == "GET":
			a.s.llmSettings(rw, r, se)
		case p == "preferences/curator" && r.Method == "PUT":
			a.s.saveLLM(rw, r, se)
		case p == "preferences/curator/models" && r.Method == "POST":
			a.s.discoverLLM(rw, r, se)
		default:
			verror(w, 404, "not_found", "Unknown curator endpoint.")
			return
		}
		if rw.status != 200 {
			verror(w, rw.status, "upstream_rejected", "Curator request failed; check provider, endpoint and model.")
			return
		}
		var v any
		_ = json.Unmarshal(rw.body.Bytes(), &v)
		dataOut(w, 200, v)
		return
	}
	if p == "mood/jobs" && r.Method == "POST" {
		var in struct {
			Seed string `json:"seedTrackId"`
		}
		if decode(r, &in) != nil || in.Seed == "" || len(in.Seed) > 1024 || se.ServerURL == "" {
			verror(w, 422, "validation_failed", "Choose a library seed track.")
			return
		}
		llm, e := a.s.effectiveLLM(se)
		if e != nil || llm == nil || llm.Provider == "off" {
			verror(w, 409, "unsupported", "Configure a curator before building Mood.")
			return
		}
		a.s.mu.Lock()
		key := moodKey(se)
		if _, ok := a.s.moodJobs[key]; ok {
			a.s.mu.Unlock()
			verror(w, 409, "version_conflict", "Mood is already running for this library user.")
			return
		}
		a.s.moodJobs[key] = ""
		a.s.mu.Unlock()
		job := nativeMood{ID: randomID(), State: "running", Username: se.User, Device: se.DeviceID, Events: []nativeMoodEvent{}}
		if a.put("mood", job.ID, accountID(se.User), job) != nil {
			a.s.mu.Lock()
			delete(a.s.moodJobs, key)
			a.s.mu.Unlock()
			verror(w, 503, "storage_full", "Could not save Mood job.")
			return
		}
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
		if a.moodCancel == nil {
			a.moodCancel = map[string]context.CancelFunc{}
		}
		a.moodCancel[job.ID] = cancel
		copy := *se
		copy.ctx = ctx
		copy.cancel = cancel
		a.wg.Add(1)
		go a.runNativeMood(ctx, &copy, *llm, in.Seed, job.ID, key)
		dataOut(w, 201, map[string]string{"jobId": job.ID, "state": "running"})
		return
	}
	parts := strings.Split(p, "/")
	if len(parts) < 3 || parts[0] != "mood" || parts[1] != "jobs" {
		verror(w, 404, "not_found", "Unknown Mood endpoint.")
		return
	}
	id := parts[2]
	if r.Method == "DELETE" {
		var job nativeMood
		if a.get("mood", id, accountID(se.User), &job) != nil {
			verror(w, 404, "not_found", "Mood job not found.")
			return
		}
		if cancel := a.moodCancel[id]; cancel != nil {
			cancel()
		}
		w.WriteHeader(204)
		return
	}
	if r.Method != "GET" {
		verror(w, 405, "unsupported", "Unsupported Mood method.")
		return
	}
	a.mu.Lock()
	var job nativeMood
	e := a.get("mood", id, accountID(se.User), &job)
	a.mu.Unlock()
	if e != nil {
		verror(w, 404, "not_found", "Mood job not found.")
		return
	}
	if len(parts) == 3 {
		dataOut(w, 200, job)
		return
	}
	if len(parts) != 4 || parts[3] != "events" {
		verror(w, 404, "not_found", "Unknown Mood endpoint.")
		return
	}
	cursor := 0
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		_, _ = fmt.Sscan(v, &cursor)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	if _, e = fmt.Fprint(w, ": ready\n\n"); e != nil {
		return
	}
	if controller.Flush() != nil {
		return
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		a.mu.Lock()
		e = a.get("mood", id, accountID(se.User), &job)
		a.mu.Unlock()
		if e != nil {
			return
		}
		for _, event := range job.Events {
			if event.Sequence <= cursor {
				continue
			}
			b, _ := json.Marshal(event)
			if _, e = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, b); e != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
			cursor = event.Sequence
		}
		if job.State != "running" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-a.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *apiV1) runNativeMood(ctx context.Context, se *session, llm llmConfig, seed, id, key string) {
	defer a.wg.Done()
	defer func() {
		se.cancel()
		a.s.mu.Lock()
		delete(a.s.moodJobs, key)
		a.s.mu.Unlock()
		a.mu.Lock()
		delete(a.moodCancel, id)
		a.mu.Unlock()
	}()
	emit := func(event moodEvent) error {
		a.mu.Lock()
		defer a.mu.Unlock()
		var job nativeMood
		if e := a.get("mood", id, accountID(se.User), &job); e != nil {
			return e
		}
		kind := map[string]string{"started": "progress", "track": "selection", "done": "completed", "error": "failed"}[event.Type]
		e := nativeMoodEvent{Sequence: len(job.Events) + 1, Type: kind, Playlist: event.Playlist, Count: event.Count, Target: event.Target, Message: event.Error}
		if event.Track != nil {
			b, _ := json.Marshal(event.Track)
			e.Track = normalize("track", object(b))
		}
		job.Events = append(job.Events, e)
		if kind == "completed" {
			job.State = "completed"
		}
		if kind == "failed" {
			job.State = "failed"
		}
		return a.put("mood", id, accountID(se.User), job)
	}
	if err := a.s.runMood(ctx, se, llm, seed, emit); err != nil {
		msg := "Mood stopped. Confirmed tracks remain in the upstream playlist."
		if ctx.Err() != nil {
			msg = "Mood cancelled; confirmed tracks remain saved."
		}
		_ = emit(moodEvent{Type: "error", Error: msg})
	}
}

func (a *apiV1) cancelDeviceMood(device string) {
	rows, e := a.list("mood", "*")
	if e != nil {
		return
	}
	for _, row := range rows {
		var j nativeMood
		if json.Unmarshal(row, &j) == nil && j.Device == device && j.State == "running" {
			if cancel := a.moodCancel[j.ID]; cancel != nil {
				cancel()
			}
		}
	}
}
func (a *apiV1) recoverMoods() {
	rows, e := a.list("mood", "*")
	if e != nil {
		return
	}
	for _, row := range rows {
		var j nativeMood
		if json.Unmarshal(row, &j) == nil && j.State == "running" {
			j.State = "failed"
			j.Events = append(j.Events, nativeMoodEvent{Sequence: len(j.Events) + 1, Type: "failed", Message: "Server restarted; confirmed tracks remain saved."})
			_ = a.put("mood", j.ID, accountID(j.Username), j)
		}
	}
}
