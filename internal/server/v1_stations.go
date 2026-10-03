package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
)

type nativeStation struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"streamURL"`
	Preset  bool   `json:"preset"`
	Order   int    `json:"order"`
	Version int64  `json:"version"`
}
type stationCollection struct {
	Version  int64           `json:"version"`
	Stations []nativeStation `json:"stations"`
}

func (a *apiV1) collection(se *session) (stationCollection, error) {
	var c stationCollection
	key := se.stateKey()
	if e := a.get("stations", key, accountID(se.User), &c); e == nil {
		return c, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return c, e
	}
	old, e := a.s.store.read(key)
	if e != nil {
		return c, e
	}
	c.Version = 1
	c.Stations = []nativeStation{}
	for i, s := range old.Stations {
		c.Stations = append(c.Stations, nativeStation{"station-" + digest(key + s.URL)[:24], s.Name, s.URL, s.Preset, i, 1})
	}
	return c, a.put("stations", key, accountID(se.User), c)
}
func (a *apiV1) stations(w http.ResponseWriter, r *http.Request, se *session) {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/radio/")
	if p == "directory" && r.Method == "GET" {
		if r.URL.Query().Get("cursor") != "" {
			a.page(w, r, se, nil)
			return
		}
		q := r.URL.Query()
		q.Set("source", q.Get("provider"))
		q.Set("q", q.Get("query"))
		r.URL.RawQuery = q.Encode()
		b := httptestResponse()
		a.s.directory(b, r, se)
		if b.status != 200 {
			verror(w, b.status, "upstream_unavailable", "Radio directory is unavailable.")
			return
		}
		var rows []any
		_ = json.Unmarshal(b.body.Bytes(), &rows)
		a.page(w, r, se, rows)
		return
	}
	if p == "events" && r.Method == "GET" {
		q := r.URL.Query()
		q.Set("playback", q.Get("playbackId"))
		r.URL.RawQuery = q.Encode()
		a.radioEvents(w, r, se)
		return
	}
	// Mutation dispatch already owns the write lock.
	if r.Method == "GET" {
		a.mu.Lock()
		defer a.mu.Unlock()
	}
	c, e := a.collection(se)
	if e != nil {
		verror(w, 503, "storage_full", "Could not read saved stations.")
		return
	}
	id := strings.TrimPrefix(p, "stations/")
	index := -1
	for i, s := range c.Stations {
		if s.ID == id {
			index = i
		}
	}
	switch {
	case p == "stations" && r.Method == "GET":
		w.Header().Set("ETag", etag(c.Version))
		dataOut(w, 200, c.Stations)
		return
	case p == "stations" && r.Method == "POST":
		var in struct {
			Name   string `json:"name"`
			URL    string `json:"streamURL"`
			Preset bool   `json:"preset"`
		}
		if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 || len(in.URL) > 2048 || validRadioURL(in.URL) != nil || len(c.Stations) >= 200 {
			verror(w, 422, "validation_failed", "Enter a station name and public HTTP(S) audio URL.")
			return
		}
		for _, s := range c.Stations {
			if s.URL == in.URL {
				verror(w, 409, "version_conflict", "Station URL is already saved.")
				return
			}
		}
		c.Stations = append(c.Stations, nativeStation{randomID(), in.Name, in.URL, in.Preset, len(c.Stations), 1})
		index = len(c.Stations) - 1
	case p == "presets" && r.Method == "PUT":
		if !precondition(w, r, c.Version) {
			return
		}
		var in struct {
			IDs []string `json:"stationIds"`
		}
		if decode(r, &in) != nil || len(in.IDs) > 8 {
			verror(w, 422, "validation_failed", "Choose up to eight unique saved stations.")
			return
		}
		chosen := map[string]int{}
		for n, id := range in.IDs {
			if _, ok := chosen[id]; ok {
				verror(w, 422, "validation_failed", "Duplicate station.")
				return
			}
			found := false
			for _, s := range c.Stations {
				if s.ID == id {
					found = true
				}
			}
			if !found {
				verror(w, 422, "validation_failed", "Unknown station.")
				return
			}
			chosen[id] = n
		}
		next := len(in.IDs)
		for i := range c.Stations {
			order, ok := chosen[c.Stations[i].ID]
			if !ok {
				order = next
				next++
			}
			c.Stations[i].Preset = ok
			c.Stations[i].Order = order
			c.Stations[i].Version++
		}
		sort.SliceStable(c.Stations, func(i, j int) bool { return c.Stations[i].Order < c.Stations[j].Order })
	case strings.HasPrefix(p, "stations/") && (r.Method == "PATCH" || r.Method == "DELETE"):
		if index < 0 {
			verror(w, 404, "not_found", "Station not found.")
			return
		}
		if !precondition(w, r, c.Stations[index].Version) {
			return
		}
		if r.Method == "DELETE" {
			c.Stations = append(c.Stations[:index], c.Stations[index+1:]...)
			index = -1
		} else {
			var in struct {
				Name   *string `json:"name"`
				URL    *string `json:"streamURL"`
				Preset *bool   `json:"preset"`
			}
			if decode(r, &in) != nil {
				verror(w, 422, "validation_failed", "Invalid station fields.")
				return
			}
			s := &c.Stations[index]
			if in.Name != nil {
				s.Name = *in.Name
			}
			if in.URL != nil {
				s.URL = *in.URL
			}
			if in.Preset != nil {
				s.Preset = *in.Preset
			}
			if strings.TrimSpace(s.Name) == "" || len(s.Name) > 200 || len(s.URL) > 2048 || validRadioURL(s.URL) != nil {
				verror(w, 422, "validation_failed", "Invalid name or stream URL.")
				return
			}
			s.Version++
		}
	default:
		verror(w, 404, "not_found", "Unknown radio endpoint.")
		return
	}
	presets := 0
	urls := map[string]bool{}
	for i := range c.Stations {
		c.Stations[i].Order = i
		s := c.Stations[i]
		if s.Preset {
			presets++
		}
		if urls[s.URL] {
			verror(w, 422, "validation_failed", "Station URLs must be unique.")
			return
		}
		urls[s.URL] = true
	}
	if presets > 8 {
		verror(w, 422, "validation_failed", "Only eight saved presets are allowed.")
		return
	}
	c.Version++
	if a.put("stations", se.stateKey(), accountID(se.User), c) != nil {
		verror(w, 503, "storage_full", "Could not save stations.")
		return
	}
	w.Header().Set("ETag", etag(c.Version))
	if r.Method == "DELETE" {
		w.WriteHeader(204)
	} else if index >= 0 {
		w.Header().Set("ETag", etag(c.Stations[index].Version))
		code := 200
		if r.Method == "POST" {
			code = 201
		}
		dataOut(w, code, c.Stations[index])
	} else {
		dataOut(w, 200, c.Stations)
	}
}
func httptestResponse() *bufferedResponse { return &bufferedResponse{head: http.Header{}} }
func (a *apiV1) queueAPI(w http.ResponseWriter, r *http.Request, se *session) {
	if se.DeviceID == "" {
		verror(w, 422, "validation_failed", "Queue requires a device session.")
		return
	}
	key := accountID(se.User) + a.libraryID(se) + se.DeviceID
	if r.Method == "GET" {
		var state userState
		_ = a.get("queue", key, accountID(se.User), &state)
		if state.Queue == nil {
			state.Queue = []json.RawMessage{}
		}
		dataOut(w, 200, map[string]any{"queue": state.Queue, "current": state.Current})
		return
	}
	if r.Method != "PUT" {
		verror(w, 405, "unsupported", "Use GET or PUT.")
		return
	}
	var state userState
	if decode(r, &state) != nil || len(state.Queue) > 1000 || !validSavedTrack(state.Current, true) {
		verror(w, 422, "validation_failed", "Invalid queue.")
		return
	}
	for _, t := range state.Queue {
		if !validSavedTrack(t, false) {
			verror(w, 422, "validation_failed", "Invalid queue track.")
			return
		}
	}
	state.Stations = nil
	if a.put("queue", key, accountID(se.User), state) != nil {
		verror(w, 503, "storage_full", "Could not save queue.")
		return
	}
	dataOut(w, 200, map[string]any{"queue": state.Queue, "current": state.Current})
}
