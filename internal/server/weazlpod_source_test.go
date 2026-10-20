package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The fake server may change bytes while keeping the same track metadata and ID.
func TestPodSourceReplacementAndDedup(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "pod")
	s := h.app.Config.Handler.(*Server)
	original := s.upstream.Transport
	var generation atomic.Int32
	var sources [2][]byte
	for i, frequency := range []string{"440", "880"} {
		file := filepath.Join(t.TempDir(), "source.wav")
		if err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency="+frequency+":sample_rate=48000", "-t", "1", file).Run(); err != nil {
			t.Fatal(err)
		}
		sources[i], _ = os.ReadFile(file)
	}
	gate := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "stream") {
			w.Header().Set("Content-Type", "audio/mp4")
			f, err := os.Open(filepath.Join("..", "..", "fixtures", "v1", "media", "aac-lc-160k-stereo.m4a"))
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			_, _ = io.Copy(w, f)
			return
		}
		if strings.Contains(r.URL.Path, "download") {
			<-gate
			if r.Header.Get("Authorization") != "" || r.Header.Get("X-Weazl-Client") != "" {
				t.Error("forwarded client credentials")
			}

			_, _ = w.Write(sources[generation.Load()])
			return
		}
		t.Error("unexpected upstream")
		w.WriteHeader(500)
	}))
	defer upstream.Close()
	s.upstream.Transport = podDownloadTransport{base: original, target: upstream.URL}
	generation.Store(0)
	body := map[string]string{"kind": "trackStream", "resourceId": "0-0", "quality": podProfile}
	code, b, _ := h.vrequest("POST", "media/leases", body, c.Access, nil)
	if code != 202 {
		t.Fatal(code, string(b))
	}
	code2, b2, _ := h.vrequest("POST", "media/leases", body, c.Access, nil)
	if code2 != 202 || decodeData[podPreparation](t, b).ID != decodeData[podPreparation](t, b2).ID {
		t.Fatal("preparation not deduplicated")
	}
	close(gate)
	first := podPoll(t, h, c.Access, code, b)
	// A legacy player receives an upstream default AAC stream, but original download
	// and the MP3 source both still come from byte-identical WAV download bytes.
	for _, kind := range []string{"trackStream", "trackOriginal"} {
		code, b, _ := h.vrequest("POST", "media/leases", map[string]string{"kind": kind, "resourceId": "0-0"}, c.Access, nil)
		if code != 200 {
			t.Fatal(code, string(b))
		}
		lease := decodeData[map[string]any](t, b)
		_, raw, headers := podGet(t, h, "GET", lease["url"].(string), nil)
		if kind == "trackOriginal" && !bytes.Equal(raw, sources[0]) {
			t.Fatal("original replaced by upstream default transcode")
		}
		if kind == "trackStream" && headers.Get("Content-Type") != "audio/mp4" {
			t.Fatal("fixture failed to apply upstream stream default")
		}
	}

	// No overlap with the previous download: polling ready synchronizes this mutation.
	generation.Store(1)
	code, b, _ = h.vrequest("POST", "media/leases", body, c.Access, nil)
	second := podPoll(t, h, c.Access, code, b)
	if first["representationId"] == second["representationId"] || first["sha256"] == second["sha256"] {
		t.Fatal("source replacement reused bytes")
	}
	_, oldBytes, _ := podGet(t, h, "GET", first["url"].(string), nil)
	sum := sha256.Sum256(oldBytes)
	if hex.EncodeToString(sum[:]) != first["sha256"] {
		t.Fatal("old representation changed")
	}
}

type podDownloadTransport struct {
	base   http.RoundTripper
	target string
}

func (p podDownloadTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !strings.Contains(r.URL.Path, "download") && !strings.Contains(r.URL.Path, "stream") {
		return p.base.RoundTrip(r)
	}
	clone := r.Clone(r.Context())
	u := *clone.URL
	target, _ := http.NewRequest("GET", p.target, nil)
	u.Host = target.URL.Host
	u.Scheme = target.URL.Scheme
	clone.URL = &u
	return p.base.RoundTrip(clone)
}
