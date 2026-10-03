// Package testradio supplies deterministic synthetic audio. Its trusted transport
// is injected only by fixture programs and tests, never through production flags.
package testradio

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	mu          sync.Mutex
	Audio       map[int][]byte
	Connections map[string]int
	Epoch       time.Time
}

func New() (*Server, error) {
	s := &Server{Audio: map[int][]byte{}, Connections: map[string]int{}, Epoch: time.Now()}
	for i := 0; i < 6; i++ {
		expression := fmt.Sprintf("aevalsrc=0.15*sin(2*PI*(%d+40*floor(t/10))*t):s=48000:d=60", 440+i*110)
		b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", expression, "-c:a", "libmp3lame", "-b:a", "128k", "-write_xing", "0", "-id3v2_version", "0", "-f", "mp3", "pipe:1").Output()
		if e != nil {
			return nil, e
		}
		s.Audio[i] = b
	}
	return s, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/station/"))
	if id < 0 || id >= 6 {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	s.Connections[r.URL.Path]++
	attempt := s.Connections[r.URL.Path]
	s.mu.Unlock()
	if r.URL.Query().Get("failed") == "1" {
		w.WriteHeader(503)
		return
	}
	audio := s.Audio[id]
	w.Header().Set("Content-Type", "audio/mpeg")
	icy := r.Header.Get("Icy-MetaData") == "1"
	if icy {
		w.Header().Set("Icy-Metaint", "16000")
		w.Header().Set("Icy-Name", fmt.Sprintf("Fixture %d", id+1))
	}
	w.(http.Flusher).Flush()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	offset := int(time.Since(s.Epoch).Seconds()*16000) % len(audio)
	offset -= offset % 384
	sent := 0
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			chunk := make([]byte, 320)
			for i := range chunk {
				chunk[i] = audio[(offset+i)%len(audio)]
			}
			offset = (offset + len(chunk)) % len(audio)
			if _, e := w.Write(chunk); e != nil {
				return
			}
			sent += len(chunk)
			if icy && sent%16000 == 0 {
				title := fmt.Sprintf("StreamTitle='Fixture Artist - Station %d cue %d';", id+1, sent/160000)
				padding := ((len(title) + 15) / 16) * 16
				b := append([]byte{byte(padding / 16)}, append([]byte(title), bytes.Repeat([]byte{0}, padding-len(title))...)...)
				if _, e := w.Write(b); e != nil {
					return
				}
			}
			w.(http.Flusher).Flush()
			if r.URL.Query().Get("reconnect") == "1" && attempt == 1 && sent >= 64000 {
				return
			}
			if r.URL.Query().Get("stall") == "1" && attempt == 1 && sent >= 64000 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(20 * time.Second):
					return
				}
			}
		}
	}
}

type Transport struct {
	Target *url.URL
	Base   http.RoundTripper
}

func (t Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Hostname() != "radio.fixture.invalid" {
		return nil, fmt.Errorf("fixture destination not allowed")
	}
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = t.Target.Scheme
	u.Host = t.Target.Host
	copy.URL = &u
	copy.Header = copy.Header.Clone()
	copy.Header.Del("Authorization")
	copy.Header.Del("Cookie")
	return t.Base.RoundTrip(copy)
}
func Run(ctx context.Context) error { return ctx.Err() }
