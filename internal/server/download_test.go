package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserOriginalAndCollectionDownloads(t *testing.T) {
	h := setup(t)
	alice := h.login("alice")
	for _, path := range []string{"/api/media/download?id=0-0", "/api/download/album?id=0"} {
		if code, _, _ := h.request("GET", path, nil, nil); code != 401 {
			t.Fatal("unauthenticated download", code)
		}
	}
	// A player default can differ from the original; downloads must never call it.
	h.navHTTP.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/stream.") {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("transcoded-player-default"))
			return
		}
		if strings.Contains(r.URL.Path, "/download.") {
			w.Header().Set("ETag", `"original-v1"`)
		}
		h.nav.ServeHTTP(w, r)
	})
	code, original, res := h.request("GET", "/api/media/download?id=0-0", nil, alice)
	_, params, err := mime.ParseMediaType(res.Header.Get("Content-Disposition"))
	if code != 200 || err != nil || params["filename"] != "Tycho - Track 1.wav" || res.Header.Get("ETag") != `"original-v1"` || !bytes.HasPrefix(original, []byte("RIFF")) {
		t.Fatalf("original %d %v %v", code, params, err)
	}
	req, _ := http.NewRequest("GET", h.app.URL+"/api/media/download?id=0-0", nil)
	req.AddCookie(alice)
	req.Header.Set("Range", "bytes=10-29")
	req.Header.Set("If-Range", `"original-v1"`)
	part, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(part.Body)
	part.Body.Close()
	if part.StatusCode != 206 || !bytes.Equal(b, original[10:30]) || part.Header.Get("Content-Range") == "" {
		t.Fatal("range download")
	}
	code, body, head := h.request("HEAD", "/api/media/download?id=0-0", nil, alice)
	if code != 200 || len(body) != 0 || head.ContentLength != int64(len(original)) {
		t.Fatal("original HEAD", code, head.ContentLength)
	}
	for _, path := range []string{"/api/download/album?id=0"} {
		code, b, res = h.request("GET", path, nil, alice)
		if code != 200 || res.Header.Get("Content-Type") != "application/zip" {
			t.Fatal("zip", code, string(b))
		}
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		if len(zr.File) != 3 {
			t.Fatal("incomplete album", len(zr.File))
		}
		for i, f := range zr.File {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			r.Close()
			if err != nil || !bytes.Equal(data, original) || f.Method != zip.Store || !strings.HasPrefix(f.Name, []string{"001", "002", "003"}[i]) {
				t.Fatal("changed/misordered original", f.Name, err)
			}
		}
	}
	code, b, _ = h.request("POST", "/api/playlists", map[string]any{"name": "Duplicates / mixtape", "songIds": []string{"0-0", "0-0", "1-0"}}, alice)
	var p struct {
		Playlist struct {
			ID string `json:"id"`
		} `json:"playlist"`
	}
	json.Unmarshal(b, &p)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, b, res = h.request("GET", "/api/download/playlist?id="+url.QueryEscape(p.Playlist.ID), nil, alice)
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if code != 200 || err != nil || len(zr.File) != 3 || zr.File[0].Name == zr.File[1].Name {
		t.Fatal("playlist duplicates lost", code, err)
	}
	_, params, _ = mime.ParseMediaType(res.Header.Get("Content-Disposition"))
	if params["filename"] != "Duplicates _ mixtape.zip" {
		t.Fatal(params)
	}
	for path, want := range map[string]int{"/api/media/download": 400, "/api/media/download?id=missing": 404, "/api/download/album": 400, "/api/download/artist?id=0": 404, "/api/download/album?id=missing": 404} {
		if code, _, _ := h.request("GET", path, nil, alice); code != want {
			t.Fatal(path, code, want)
		}
	}
}

func TestBrowserDownloadFailuresAndLimits(t *testing.T) {
	h := setup(t)
	alice := h.login("alice")
	bob := h.login("bob")
	var mode atomic.Int32
	h.navHTTP.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		method := r.URL.Path
		if strings.Contains(method, "getAlbum.") || strings.Contains(method, "getPlaylist.") {
			kind, key := "album", "song"
			if strings.Contains(method, "getPlaylist.") {
				kind, key = "playlist", "entry"
			}
			if q.Get("u") == "bob" {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"subsonic-response":{"status":"failed","error":{"code":50}}}`)
				return
			}
			count := 2
			if mode.Load() == 1 {
				count = 1001
			}
			if mode.Load() == 2 {
				count = 0
			}
			songs := make([]downloadSong, count)
			for i := range songs {
				songs[i] = downloadSong{ID: []string{"0-0", "0-1"}[i%2], Artist: "../escape", Title: "line\nname", Suffix: "mp3"}
			}
			json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{"status": "ok", kind: map[string]any{"id": "0", "name": "../album", key: songs}}})
			return
		}
		if strings.Contains(method, "download.") {
			if mode.Load() == 3 || mode.Load() == 4 && q.Get("id") == "0-1" {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"error":"denied"}`)
				return
			}
		}
		h.nav.ServeHTTP(w, r)
	})
	if code, _, _ := h.request("GET", "/api/download/playlist?id=0", nil, bob); code != 404 {
		t.Fatal("upstream authorization", code)
	}
	mode.Store(1)
	if code, _, _ := h.request("GET", "/api/download/album?id=0", nil, alice); code != 422 {
		t.Fatal("track cap", code)
	}
	mode.Store(2)
	if code, _, _ := h.request("GET", "/api/download/album?id=0", nil, alice); code != 422 {
		t.Fatal("empty collection", code)
	}
	mode.Store(3)
	if code, _, r := h.request("GET", "/api/download/album?id=0", nil, alice); code != 502 || r.Header.Get("Content-Disposition") != "" {
		t.Fatal("error downloaded as album", code)
	}
	if code, _, r := h.request("GET", "/api/media/download?id=0-0", nil, alice); code != 502 || r.Header.Get("Content-Disposition") != "" {
		t.Fatal("error downloaded as song", code)
	}
	mode.Store(4)
	req, _ := http.NewRequest("GET", h.app.URL+"/api/download/album?id=0", nil)
	req.AddCookie(alice)
	res, err := http.DefaultClient.Do(req)
	if err == nil {
		b, e := io.ReadAll(res.Body)
		res.Body.Close()
		if e == nil {
			t.Fatal("interrupted ZIP did not fail transport")
		}
		if _, e = zip.NewReader(bytes.NewReader(b), int64(len(b))); e == nil {
			t.Fatal("valid but incomplete ZIP")
		}
	}
	// Exhaust only the ZIP semaphore; individual media remains independent.
	s := h.app.Config.Handler.(*Server)
	s.downloadSlots <- struct{}{}
	s.downloadSlots <- struct{}{}
	if code, _, r := h.request("GET", "/api/download/album?id=0", nil, alice); code != 429 || r.Header.Get("Retry-After") != "5" {
		t.Fatal("concurrency cap", code)
	}
	<-s.downloadSlots
	<-s.downloadSlots
	mode.Store(0)
	if code, _, _ := h.request("HEAD", "/api/download/album?id=0", nil, alice); code != 200 {
		t.Fatal("slot recovery", code)
	}
}

func TestDownloadFilenameSafety(t *testing.T) {
	song := downloadSong{Artist: "../../bad\r\n", Title: `a\b:c<d>"e|f?g*`, Suffix: "../../flac"}
	name := song.filename()
	if strings.ContainsAny(name, "/\\\r\n:<>\"|?*") || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".bin") {
		t.Fatal(name)
	}
	if got := (downloadSong{Title: "AAC", Suffix: "m4a"}).filename(); !strings.HasSuffix(got, ".m4a") {
		t.Fatal(got)
	}
}

func TestBrowserDownloadCancellation(t *testing.T) {
	for _, logout := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "logout"}[logout], func(t *testing.T) {
			h := setup(t)
			alice := h.login("alice")
			cancelled := make(chan struct{}, 1)
			h.navHTTP.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "download.") {
					w.Header().Set("Content-Type", "audio/wav")
					w.Write(bytes.Repeat([]byte("RIFF"), 8192))
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					cancelled <- struct{}{}
					return
				}
				h.nav.ServeHTTP(w, r)
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", h.app.URL+"/api/download/album?id=0", nil)
			req.AddCookie(alice)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if logout {
				if code, _, _ := h.request("POST", "/api/logout", map[string]any{}, alice); code != 200 {
					t.Fatal(code)
				}
			} else {
				cancel()
			}
			res.Body.Close()
			select {
			case <-cancelled:
			case <-time.After(3 * time.Second):
				t.Fatal("original upstream survived cancellation")
			}
			s := h.app.Config.Handler.(*Server)
			deadline := time.Now().Add(3 * time.Second)
			for len(s.downloadSlots) > 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if len(s.downloadSlots) != 0 {
				t.Fatal("ZIP slot leaked after cancellation")
			}
		})
	}
}
