package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type localGroup struct{ sync.WaitGroup }

func (g *localGroup) goRun(f func()) { g.Add(1); go func() { defer g.Done(); f() }() }
func (g *localGroup) wait()          { g.Wait() }
func mediaToolsAvailable() bool {
	_, e := exec.LookPath("ffmpeg")
	_, ee := exec.LookPath("ffprobe")
	_, fdkErr := exec.LookPath("fdkaac")
	return e == nil && ee == nil && fdkErr == nil
}
func (a *apiV1) captureStation(parent context.Context, j recordingJob, index int) {
	root := filepath.Join(a.s.cfg.DataDir, "recordings", "staging", j.ID, j.Stations[index].StationID)
	if os.MkdirAll(root, 0700) != nil {
		return
	}
	// Recover only FFmpeg's completed segment list; partially written MP4 files are never promoted.
	attempts, _ := os.ReadDir(root)
	for _, entry := range attempts {
		if entry.IsDir() {
			a.recoverAttempt(j, index, filepath.Join(root, entry.Name()))
		}
	}
	backoff := time.Second
	for parent.Err() == nil && j.Ends.After(a.now()) {
		ctx, cancel := context.WithDeadline(parent, j.Ends)
		e := a.captureAttempt(ctx, j, index, root)
		cancel()
		if e != nil {
			a.stationError(j.ID, index, "Stream interrupted; retrying within the original window.")
		}
		if parent.Err() != nil || !j.Ends.After(a.now()) {
			return
		}
		wait := min(backoff, j.Ends.Sub(a.now()))
		timer := time.NewTimer(wait)
		select {
		case <-parent.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

type guardedAudio struct {
	io.Reader
	last  atomic.Int64
	first atomic.Int64
	count atomic.Int64
	max   int64
	ctx   context.Context
	check func() bool
}

func (g *guardedAudio) Read(p []byte) (int, error) {
	if g.ctx.Err() != nil {
		return 0, g.ctx.Err()
	}
	if g.count.Load() > g.max {
		return 0, errors.New("station byte budget exceeded")
	}
	n, e := g.Reader.Read(p)
	if n > 0 {
		now := time.Now().UnixNano()
		g.first.CompareAndSwap(0, now)
		g.last.Store(now)
		total := g.count.Add(int64(n))
		if total%(1<<18) < int64(n) && !g.check() {
			return 0, errors.New("disk reserve reached")
		}
	}
	return n, e
}
func (a *apiV1) captureAttempt(ctx context.Context, j recordingJob, index int, root string) error {
	res, e := a.s.resolveRadio(ctx, j.Stations[index].Station.URL)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	// Network access is confined to the validated Go relay. FFmpeg sees a pipe, never a URL or credential.
	g := &guardedAudio{Reader: res.Body, ctx: ctx, max: estimateBytes(j.Ends.Sub(a.now()), 1) * 2, check: func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		v, e := a.storage()
		return e == nil && v.Used < v.Budget && diskReserveOK(a.s.cfg.DataDir, v.Reserve)
	}}
	g.last.Store(time.Now().UnixNano())
	attempt := filepath.Join(root, randomID())
	if e = os.MkdirAll(attempt, 0700); e != nil {
		return e
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				res.Body.Close()
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, g.last.Load())) > 15*time.Second {
					res.Body.Close()
					return
				}
			}
		}
	}()
	seconds := a.s.cfg.SegmentSeconds
	if seconds <= 0 {
		seconds = 30
	}
	pipeline, e := startRecorderEncoder(attempt, seconds)
	if e != nil {
		return e
	}
	pipe := pipeline.Input
	processDone := make(chan error, 1)
	go func() { processDone <- pipeline.wait() }()
	copyDone := make(chan struct{})
	var relayErr error
	go func() {
		defer close(copyDone)
		defer pipe.Close()
		interval, e := strconv.ParseInt(res.Header.Get("Icy-Metaint"), 10, 64)
		if res.Header.Get("Icy-Metaint") != "" {
			if e != nil || interval <= 0 || interval > 1<<20 {
				relayErr = errors.New("invalid ICY interval")
				return
			}
			relayErr = relayICY(pipe, g, interval, func(meta radioMetadata) { a.captureMetadata(j.ID, index, meta) }, func() {})
		} else {
			_, relayErr = io.Copy(pipe, g)
		}
	}()
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	anchored := false
	var processErr error
	for {
		select {
		case <-timer.C:
			if !anchored && g.first.Load() != 0 {
				base := time.Unix(0, g.first.Load()).Sub(j.Starts).Milliseconds()
				b, _ := json.Marshal(max(int64(0), base))
				if writeDurable(filepath.Join(attempt, "anchor.json"), b) == nil {
					anchored = true
				}
			}
			if anchored {
				a.recoverAttempt(j, index, attempt)
			}
		case processErr = <-processDone:
			res.Body.Close()
			<-copyDone
			if !anchored && g.first.Load() != 0 {
				b, _ := json.Marshal(max(int64(0), time.Unix(0, g.first.Load()).Sub(j.Starts).Milliseconds()))
				_ = writeDurable(filepath.Join(attempt, "anchor.json"), b)
			}
			a.recoverAttempt(j, index, attempt)
			if processErr != nil {
				return errors.New("media packaging failed")
			}
			return relayErr
		case <-ctx.Done():
			res.Body.Close()
			pipe.Close()
			select {
			case processErr = <-processDone:
			case <-time.After(3 * time.Second):
				pipeline.kill()
				processErr = <-processDone
			}
			<-copyDone
			if !anchored && g.first.Load() != 0 {
				b, _ := json.Marshal(max(int64(0), time.Unix(0, g.first.Load()).Sub(j.Starts).Milliseconds()))
				_ = writeDurable(filepath.Join(attempt, "anchor.json"), b)
			}
			a.recoverAttempt(j, index, attempt)
			return processErr
		}
	}
}
func (a *apiV1) stationError(id string, index int, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var j recordingJob
	if a.get("job", id, "*", &j) != nil {
		return
	}
	j.Stations[index].Error = message
	j.Stations[index].State = "retrying"
	j.Version++
	_ = a.put("job", id, accountID(j.Username), j)
}
func (a *apiV1) captureMetadata(id string, index int, meta radioMetadata) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var j recordingJob
	if a.get("job", id, "*", &j) != nil {
		return
	}
	p := &j.Stations[index]
	offset := a.now().Sub(j.Starts).Milliseconds()
	if offset < 0 || offset >= j.Duration {
		return
	}
	if len(p.Metadata) > 0 {
		last := p.Metadata[len(p.Metadata)-1]
		if last.Title == meta.Title && last.Artist == meta.Artist {
			return
		}
	}
	if len(p.Metadata) >= 10000 {
		return
	}
	p.Metadata = append(p.Metadata, recordingMetadata{offset, meta.Title, meta.Artist})
	j.Version++
	_ = a.put("job", id, accountID(j.Username), j)
}

var _ *http.Response
