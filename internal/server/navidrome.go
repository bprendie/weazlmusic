package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *Server) endpoint(se *session, method string, q url.Values) string {
	v := url.Values{}
	for k, values := range q {
		v[k] = append([]string(nil), values...)
	}
	v.Set("u", se.NavUser)
	v.Set("t", se.Token)
	v.Set("s", se.Salt)
	v.Set("v", "1.16.1")
	v.Set("c", "weazltunes-web")
	v.Set("f", "json")
	return se.ServerURL + "/rest/" + method + ".view?" + v.Encode()
}
func (s *Server) call(ctx context.Context, se *session, method string, q url.Values) (map[string]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", s.endpoint(se, method, q), nil)
	if err != nil {
		return nil, errors.New("invalid upstream request")
	}
	res, err := s.upstream.Do(req)
	if err != nil {
		return nil, errors.New("Navidrome is unreachable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("Navidrome returned HTTP %d", res.StatusCode)
	}
	var body struct {
		Response map[string]json.RawMessage `json:"subsonic-response"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&body); err != nil {
		return nil, errors.New("invalid Navidrome response")
	}
	if string(body.Response["status"]) != `"ok"` {
		var e struct {
			Code int `json:"code"`
		}
		_ = json.Unmarshal(body.Response["error"], &e)
		return nil, fmt.Errorf("Navidrome rejected the request (code %d)", e.Code)
	}
	return body.Response, nil
}

var reads = map[string][]string{
	"getAlbumList2": {"type", "size", "offset"}, "getAlbum": {"id"}, "search3": {"query", "songCount", "albumCount", "artistCount"},
	"getRandomSongs": {"size"}, "getPlaylists": {}, "getPlaylist": {"id"}, "getStarred2": {}, "getSong": {"id"},
}

func (s *Server) library(w http.ResponseWriter, r *http.Request, se *session) {
	method := r.PathValue("method")
	keys, ok := reads[method]
	if !ok {
		fail(w, 404, "Unknown library operation")
		return
	}
	q := url.Values{}
	for _, k := range keys {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	for _, k := range []string{"size", "songCount", "albumCount", "artistCount"} {
		if v := q.Get(k); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 0 || n > 100 {
				fail(w, 400, "Page size must be between 0 and 100")
				return
			}
		}
	}
	if method == "getAlbumList2" {
		if q.Get("size") == "" {
			q.Set("size", "40")
		}
		if q.Get("type") == "" {
			q.Set("type", "newest")
		}
	}
	out, err := s.call(r.Context(), se, method, q)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	jsonOut(w, 200, out)
}
func (s *Server) favorite(w http.ResponseWriter, r *http.Request, se *session) {
	var in struct {
		ID   string `json:"id"`
		Star bool   `json:"star"`
	}
	if decode(r, &in) != nil || in.ID == "" {
		fail(w, 400, "A track ID is required")
		return
	}
	method := "unstar"
	if in.Star {
		method = "star"
	}
	if _, err := s.call(r.Context(), se, method, url.Values{"id": {in.ID}}); err != nil {
		fail(w, 502, err.Error())
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (s *Server) media(w http.ResponseWriter, r *http.Request, se *session) {
	kind := r.PathValue("kind")
	method := "stream"
	q := url.Values{"id": {r.URL.Query().Get("id")}}
	if q.Get("id") == "" {
		fail(w, 400, "Missing media ID")
		return
	}
	if kind == "cover" {
		method = "getCoverArt"
		q.Set("size", "400")
	} else if kind != "stream" {
		fail(w, 404, "Unknown media")
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), se.Expires)
	defer cancel()
	stop := context.AfterFunc(se.ctx, cancel)
	defer stop()
	req, err := http.NewRequestWithContext(ctx, "GET", s.endpoint(se, method, q), nil)
	if err != nil {
		fail(w, 502, "Invalid media request")
		return
	}
	req.Header.Set("Range", r.Header.Get("Range"))
	res, err := s.upstream.Do(req)
	if err != nil {
		fail(w, 502, "Media is unavailable")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 206 && res.StatusCode != 416 {
		fail(w, 502, "Navidrome could not serve this media")
		return
	}
	ct := res.Header.Get("Content-Type")
	if kind == "cover" && !strings.HasPrefix(ct, "image/") {
		fail(w, 502, "Cover unavailable")
		return
	}
	if kind != "cover" && (strings.Contains(ct, "json") || strings.Contains(ct, "xml") || strings.Contains(ct, "html")) {
		fail(w, 502, "Navidrome rejected playback")
		return
	}
	copyMedia(w, res)
}
func copyMedia(w http.ResponseWriter, res *http.Response) {
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := res.Header.Get(key); v != "" {
			w.Header().Set(key, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	_, _ = io.Copy(w, res.Body)
}
