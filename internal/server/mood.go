package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const moodSize = 20

var moodComplete = errors.New("mood complete")

type moodEvent struct {
	Type     string        `json:"type"`
	Playlist *moodPlaylist `json:"playlist,omitempty"`
	Track    *moodTrack    `json:"track,omitempty"`
	Count    int           `json:"count,omitempty"`
	Target   int           `json:"target,omitempty"`
	Error    string        `json:"error,omitempty"`
}

func (s *Server) buildMood(w http.ResponseWriter, r *http.Request, se *session) {
	var in struct {
		SeedID string `json:"seedId"`
	}
	if decode(r, &in) != nil || in.SeedID == "" || len(in.SeedID) > 1024 {
		fail(w, 400, "Play a library track before building Mood")
		return
	}
	llm, err := s.effectiveLLM(se)
	if err != nil || llm == nil || llm.Provider == "off" {
		fail(w, 409, "Configure Ollama or vLLM in your account settings first")
		return
	}
	s.mu.Lock()
	key := moodKey(se)
	if _, busy := s.moodJobs[key]; busy {
		s.mu.Unlock()
		fail(w, 409, "Mood is already being built for this Navidrome user")
		return
	}
	s.moodJobs[key] = ""
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	stop := context.AfterFunc(se.ctx, cancel)
	defer stop()
	events := make(chan moodEvent)
	emit := func(e moodEvent) error {
		select {
		case events <- e:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	go func() {
		defer close(events)
		defer func() { s.mu.Lock(); delete(s.moodJobs, key); s.mu.Unlock() }()
		if err := s.runMood(ctx, se, *llm, in.SeedID, emit); err != nil {
			_ = emit(moodEvent{Type: "error", Error: err.Error()})
		}
	}()
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	controller := http.NewResponseController(w)
	if controller.Flush() != nil {
		return
	}
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if json.NewEncoder(w).Encode(moodEvent{Type: "working"}) != nil || controller.Flush() != nil {
				return
			}
		case e, ok := <-events:
			if !ok {
				return
			}
			if json.NewEncoder(w).Encode(e) != nil || controller.Flush() != nil {
				return
			}
		}
	}
}
func (s *Server) runMood(ctx context.Context, se *session, c llmConfig, seedID string, emit func(moodEvent) error) error {
	out, err := s.call(ctx, se, "getSong", url.Values{"id": {seedID}})
	if err != nil {
		return err
	}
	var seed moodTrack
	if json.Unmarshal(out["song"], &seed) != nil || seed.ID != seedID {
		return errors.New("The seed track is not available in this Navidrome library")
	}
	candidates, err := s.moodCandidates(ctx, se, seed)
	if err != nil {
		return err
	}
	playlist, err := s.startMoodPlaylist(ctx, se, seed)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.moodJobs[moodKey(se)] = playlist.ID
	s.mu.Unlock()
	if err = emit(moodEvent{Type: "started", Playlist: &playlist, Track: &seed, Count: 1, Target: moodSize}); err != nil {
		return err
	}
	allowed := map[string]moodTrack{}
	for _, t := range candidates {
		allowed[t.ID] = t
	}
	accepted := map[string]bool{seed.ID: true}
	count := 1
	for attempt := 0; attempt < 3 && count < moodSize; attempt++ {
		remaining := []moodTrack{}
		for _, t := range candidates {
			if !accepted[t.ID] {
				remaining = append(remaining, t)
			}
		}
		payload, _ := json.Marshal(map[string]any{"seed": seed, "candidates": remaining, "needed": moodSize - count})
		prompt := "Build a cohesive Mood playlist around the seed. Choose exactly " + strconv.Itoa(moodSize-count) + " remaining track IDs. Return only a JSON array of IDs. Preserve the seed's mood and energy, avoid abrupt style changes, and prefer variety over repeated artists/albums.\n" + string(payload)
		err = s.streamLLM(ctx, c, prompt, func(id string) error {
			t, ok := allowed[id]
			if !ok || accepted[id] {
				return nil
			}
			if count >= moodSize {
				return moodComplete
			}
			if _, e := s.call(ctx, se, "updatePlaylist", url.Values{"playlistId": {playlist.ID}, "songIdToAdd": {id}}); e != nil {
				return e
			}
			accepted[id] = true
			count++
			if e := emit(moodEvent{Type: "track", Track: &t, Count: count, Target: moodSize}); e != nil {
				return e
			}
			if count == moodSize {
				return moodComplete
			}
			return nil
		})
		if err != nil && !errors.Is(err, moodComplete) {
			return fmt.Errorf("Mood stopped at %d/%d; accepted tracks remain in Navidrome. %w", count, moodSize, err)
		}
	}
	if count != moodSize {
		return fmt.Errorf("Mood saved %d/%d tracks. The model did not supply enough unique library IDs after three attempts", count, moodSize)
	}
	return emit(moodEvent{Type: "done", Count: count, Target: moodSize})
}
