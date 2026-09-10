package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"
)

var playbackID = regexp.MustCompile(`^[a-zA-Z0-9-]{16,64}$`)

type radioHub struct {
	mu    sync.Mutex
	feeds map[string]*radioFeed
}
type radioFeed struct {
	mu        sync.Mutex
	refs      int
	active    bool
	latest    radioMetadata
	hasLatest bool
	listeners map[chan radioMetadata]bool
}

func newRadioHub() *radioHub { return &radioHub{feeds: map[string]*radioFeed{}} }
func (h *radioHub) acquire(id string) (*radioFeed, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f := h.feeds[id]
	if f == nil {
		if len(h.feeds) >= 16 {
			return nil, nil, errors.New("Too many radio players")
		}
		f = &radioFeed{listeners: map[chan radioMetadata]bool{}}
		h.feeds[id] = f
	}
	f.refs++
	release := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		f.refs--
		if f.refs == 0 {
			delete(h.feeds, id)
		}
	}
	return f, release, nil
}
func (f *radioFeed) begin() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active {
		return false
	}
	f.active = true
	return true
}
func (f *radioFeed) publish(meta radioMetadata) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hasLatest && f.latest == meta {
		return
	}
	f.latest = meta
	f.hasLatest = true
	for ch := range f.listeners {
		select {
		case <-ch:
		default:
		}
		ch <- meta
	}
}
func (f *radioFeed) subscribe() (chan radioMetadata, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.listeners) >= 2 {
		return nil, nil, errors.New("Too many metadata listeners")
	}
	ch := make(chan radioMetadata, 1)
	f.listeners[ch] = true
	if f.hasLatest {
		ch <- f.latest
	}
	return ch, func() { f.mu.Lock(); delete(f.listeners, ch); f.mu.Unlock() }, nil
}
func (s *Server) radioEvents(w http.ResponseWriter, r *http.Request, se *session) {
	id := r.URL.Query().Get("playback")
	if !playbackID.MatchString(id) {
		fail(w, 400, "Invalid playback ID")
		return
	}
	feed, release, err := se.radio.acquire(id)
	if err != nil {
		fail(w, 429, err.Error())
		return
	}
	defer release()
	updates, unsubscribe, err := feed.subscribe()
	if err != nil {
		fail(w, 429, err.Error())
		return
	}
	defer unsubscribe()
	ctx, cancel := context.WithDeadline(r.Context(), se.Expires)
	defer cancel()
	stop := context.AfterFunc(se.ctx, cancel)
	defer stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	flush := http.NewResponseController(w)
	if _, err = fmt.Fprint(w, ": ready\n\n"); err != nil {
		return
	}
	if flush.Flush() != nil {
		return
	}
	// Keep the active SSE stream alive through reverse-proxy idle timeouts.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-heartbeat.C:
			if _, err = fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			if flush.Flush() != nil {
				return
			}
		case <-ctx.Done():
			return
		case meta := <-updates:
			data, e := json.Marshal(meta)
			if e != nil {
				return
			}
			if _, e = fmt.Fprintf(w, "data: %s\n\n", data); e != nil {
				return
			}
			if flush.Flush() != nil {
				return
			}
			if meta.Ended {
				return
			}
		}
	}
}
