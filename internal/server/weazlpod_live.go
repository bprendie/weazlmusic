package server

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Live MP3 is deliberately separate from immutable finite representations.
func (a *apiV1) podLive(w http.ResponseWriter, r *http.Request, se *session, ml mediaLease, raw string) {
	if r.Method != "GET" || r.Header.Get("Range") != "" {
		verror(w, 422, "unsupported", "Live MP3 supports GET without byte ranges.")
		return
	}
	hub := a.radioHub(ml.Device, ml.BrowserHash)
	feed, release, err := hub.acquire(ml.PlaybackID)
	if err != nil {
		verror(w, 429, "rate_limited", "Too many radio players.")
		return
	}
	defer release()
	if !feed.begin() {
		verror(w, 409, "version_conflict", "This radio playback is already active.")
		return
	}
	defer feed.end()
	defer feed.publish(radioMetadata{Ended: true})
	p := a.pod
	p.mu.Lock()
	count := 0
	for _, n := range p.live {
		count += n
	}
	if count >= 2 || p.live[se.User] >= 1 {
		p.mu.Unlock()
		w.Header().Set("Retry-After", "2")
		verror(w, 429, "rate_limited", "Live transcode workers are busy.")
		return
	}
	p.live[se.User]++
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.live[se.User]--
		if p.live[se.User] == 0 {
			delete(p.live, se.User)
		}
		p.mu.Unlock()
	}()
	ctx, cancel := context.WithDeadline(r.Context(), ml.Expires)
	defer cancel()
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	res, err := a.s.resolveRadio(ctx, raw)
	if err != nil {
		verror(w, 502, "upstream_unavailable", "Station is unavailable.")
		return
	}
	defer res.Body.Close()
	interval := int64(0)
	if value := res.Header.Get("Icy-Metaint"); value != "" {
		interval, err = strconv.ParseInt(value, 10, 64)
		if err != nil || interval <= 0 || interval > 1<<20 {
			verror(w, 502, "upstream_rejected", "Invalid station metadata interval.")
			return
		}
	}
	// FFmpeg only sees stripped audio on stdin. Disable Xing for an unbounded stream.
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "pipe", "-probesize", "32768", "-analyzeduration", "1000000", "-i", "pipe:0", "-map", "0:a:0", "-vn", "-map_metadata", "-1", "-map_chapters", "-1", "-c:a", "libmp3lame", "-b:a", strconv.Itoa(podRadioBitrate(ml.Quality)) + "k", "-joint_stereo", "1", "-ar", "44100", "-ac", "2", "-threads", "1", "-id3v2_version", "0", "-write_id3v1", "0", "-write_xing", "0", "-flush_packets", "1", "-f", "mp3", "pipe:1"}
	cmd := mediaCommandContext(ctx, "ffmpeg", args...)
	input, err := cmd.StdinPipe()
	if err != nil {
		verror(w, 503, "transcode_failed", "Cannot start live encoder.")
		return
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		verror(w, 503, "transcode_failed", "Cannot start live encoder.")
		return
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		verror(w, 503, "transcode_failed", "Cannot start live encoder.")
		return
	}
	defer func() { cancel(); input.Close(); res.Body.Close(); _ = cmd.Wait() }()
	done := make(chan struct{})
	defer func() { <-done }()
	g := &guardedAudio{Reader: res.Body, ctx: ctx, max: int64((12 * time.Hour).Seconds()) * 80000, check: func() bool { return true }}
	g.last.Store(time.Now().UnixNano())
	go func() {
		defer close(done)
		defer input.Close()
		if interval > 0 {
			_ = relayICY(input, g, interval, feed.publish, func() {})
		} else {
			_, _ = io.Copy(input, g)
		}
	}()
	// Disconnect, stalls, shutdown and revocation stop both the network read and process.
	monitorDone := make(chan struct{})
	defer close(monitorDone)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-monitorDone:
				return
			case <-ctx.Done():
				res.Body.Close()
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, g.last.Load())) > 15*time.Second {
					cancel()
					res.Body.Close()
					return
				}
				if _, valid := a.leaseIdentity(ml); !valid {
					cancel()
					res.Body.Close()
					return
				}
			}
		}
	}()
	first := make([]byte, 4096)
	n, err := output.Read(first)
	if n == 0 {
		cancel()
		res.Body.Close()
		verror(w, 502, "transcode_failed", "Station could not be converted to MP3.")
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Weazl-Radio-Profile", ml.Quality)
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Del("Content-Length")
	w.Header().Del("ETag")
	w.Header().Del("Accept-Ranges")
	w.Header().Del("Content-Range")
	controller := http.NewResponseController(w)
	// Slow/disconnected consumers cannot hold a worker indefinitely.
	sink := &podLiveWriter{w: w, c: controller}
	_, _ = sink.Write(first[:n])
	if err == nil {
		_, _ = io.Copy(sink, output)
	}
	cancel()
	res.Body.Close()
	input.Close()
}

type podLiveWriter struct {
	w http.ResponseWriter
	c *http.ResponseController
}

func (w *podLiveWriter) Write(b []byte) (int, error) {
	_ = w.c.SetWriteDeadline(time.Now().Add(15 * time.Second))
	n, err := w.w.Write(b)
	if err == nil {
		err = w.c.Flush()
	}
	return n, err
}
