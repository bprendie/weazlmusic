package server

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

type downloadSong struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Suffix      string `json:"suffix"`
	ContentType string `json:"contentType"`
}

// Filenames never contain paths, control characters or reserved punctuation.
func downloadName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:<>"|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(strings.TrimSpace(name), ". ")
	if len(name) > 180 {
		name = name[:180]
		for !utf8.ValidString(name) {
			name = name[:len(name)-1]
		}
	}
	name = strings.Trim(name, ". ")
	if name == "" {
		return "WeazlMusic"
	}
	return name
}

func (t downloadSong) filename() string {
	ext := strings.ToLower(t.Suffix)
	if ext == "" {
		switch strings.Split(t.ContentType, ";")[0] {
		case "audio/mpeg":
			ext = "mp3"
		case "audio/flac", "audio/x-flac":
			ext = "flac"
		case "audio/mp4":
			ext = "m4a"
		case "audio/wav", "audio/x-wav":
			ext = "wav"
		case "audio/ogg":
			ext = "ogg"
		default:
			ext = "bin"
		}
	}
	if len(ext) > 10 || strings.IndexFunc(ext, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') }) >= 0 {
		ext = "bin"
	}
	return downloadName(t.Artist+" - "+t.Title) + "." + ext
}

func attachment(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
}

func downloadContext(r *http.Request, se *session) (context.Context, func()) {
	ctx, cancel := context.WithDeadline(r.Context(), se.Expires)
	stop := context.AfterFunc(se.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

func (s *Server) original(ctx context.Context, se *session, id string, headers http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.endpoint(se, "download", url.Values{"id": {id}}), nil)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"Range", "If-Range"} {
		req.Header.Set(key, headers.Get(key))
	}
	res, err := s.upstream.Do(req)
	if err != nil {
		return nil, errors.New("Original file is unavailable")
	}
	ct := strings.ToLower(res.Header.Get("Content-Type"))
	if (res.StatusCode != 200 && res.StatusCode != 206 && res.StatusCode != 416) || (res.StatusCode != 416 && (strings.Contains(ct, "json") || strings.Contains(ct, "xml") || strings.Contains(ct, "html"))) {
		res.Body.Close()
		return nil, errors.New("Navidrome could not serve the original file")
	}
	return res, nil
}

func (s *Server) downloadTrack(w http.ResponseWriter, r *http.Request, se *session) {
	id := r.URL.Query().Get("id")
	if id == "" {
		fail(w, 400, "Missing track ID")
		return
	}
	ctx, cancel := downloadContext(r, se)
	defer cancel()
	data, err := s.call(ctx, se, "getSong", url.Values{"id": {id}})
	var song downloadSong
	if err != nil || json.Unmarshal(data["song"], &song) != nil || song.ID == "" {
		fail(w, 404, "Track unavailable")
		return
	}
	res, err := s.original(ctx, se, id, r.Header)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	defer res.Body.Close()
	attachment(w, song.filename())
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if v := res.Header.Get(key); v != "" {
			w.Header().Set(key, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	if r.Method != "HEAD" {
		if _, err = io.Copy(w, res.Body); err != nil || ctx.Err() != nil {
			panic(http.ErrAbortHandler)
		}
	}
}

// ZIP entries are stored without recompression and copied sequentially. Only the
// ZIP directory is held in memory; audio is never buffered or staged on disk.
func (s *Server) downloadCollection(w http.ResponseWriter, r *http.Request, se *session) {
	kind, id := r.PathValue("kind"), r.URL.Query().Get("id")
	if kind != "album" && kind != "playlist" {
		fail(w, 404, "Unknown download collection")
		return
	}
	if id == "" {
		fail(w, 400, "Missing collection ID")
		return
	}
	select {
	case s.downloadSlots <- struct{}{}:
		defer func() { <-s.downloadSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		fail(w, 429, "Two collection downloads are already running. Try again shortly.")
		return
	}
	ctx, cancel := downloadContext(r, se)
	defer cancel()
	method, key := "getAlbum", "song"
	if kind == "playlist" {
		method, key = "getPlaylist", "entry"
	}
	data, err := s.call(ctx, se, method, url.Values{"id": {id}})
	var collection struct {
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Song  []downloadSong `json:"song"`
		Entry []downloadSong `json:"entry"`
	}
	if err != nil || json.Unmarshal(data[kind], &collection) != nil || collection.ID == "" {
		fail(w, 404, "Collection unavailable")
		return
	}
	songs := collection.Song
	if key == "entry" {
		songs = collection.Entry
	}
	if len(songs) == 0 {
		fail(w, 422, "This collection has no downloadable tracks")
		return
	}
	if len(songs) > 1000 {
		fail(w, 422, "Download collections of up to 1000 tracks at a time")
		return
	}
	for _, song := range songs {
		if song.ID == "" {
			fail(w, 422, "Collection contains an unavailable track")
			return
		}
	}
	if r.Method == "HEAD" {
		attachment(w, downloadName(collection.Name)+".zip")
		w.Header().Set("Content-Type", "application/zip")
		return
	}
	// Validate the first response before sending attachment headers. Later errors
	// abort the response without a ZIP central directory, never a successful subset.
	first, err := s.original(ctx, se, songs[0].ID, nil)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	defer first.Body.Close()
	if first.StatusCode != 200 {
		fail(w, 502, "Original file unavailable")
		return
	}
	attachment(w, downloadName(collection.Name)+".zip")
	w.Header().Set("Content-Type", "application/zip")
	zw := zip.NewWriter(w)
	for i, song := range songs {
		res := first
		if i > 0 {
			res, err = s.original(ctx, se, song.ID, nil)
			if err != nil {
				panic(http.ErrAbortHandler)
			}
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			panic(http.ErrAbortHandler)
		}
		entry, e := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("%03d - %s", i+1, song.filename()), Method: zip.Store})
		if e == nil {
			_, e = io.Copy(entry, res.Body)
		}
		res.Body.Close()
		if e != nil || ctx.Err() != nil {
			panic(http.ErrAbortHandler)
		}
	}
	if err = zw.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}
