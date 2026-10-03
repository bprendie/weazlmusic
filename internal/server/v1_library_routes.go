package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func (a *apiV1) library(w http.ResponseWriter, r *http.Request, se *session) {
	if se.ServerURL == "" {
		verror(w, 503, "upstream_unavailable", "No library connection is configured.")
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/library/")
	if r.Method != "GET" {
		a.musicMutation(w, r, se, p)
		return
	}
	if r.URL.Query().Get("cursor") != "" && !strings.Contains(p, "/") {
		a.page(w, r, se, nil)
		return
	}
	parts := strings.Split(p, "/")
	id := ""
	if len(parts) == 2 {
		id = parts[1]
	}
	method, key, kind := "", "", ""
	switch parts[0] {
	case "tracks":
		method, key, kind = "search3", "searchResult3", "track"
	case "albums":
		method, key, kind = "getAlbumList2", "albumList2", "album"
	case "artists":
		method, key, kind = "getArtists", "artists", "artist"
	case "favorites":
		method, key = "getStarred2", "starred2"
	case "playlists":
		method, key = "getPlaylists", "playlists"
	case "search":
		method, key = "search3", "searchResult3"
	default:
		verror(w, 404, "not_found", "Unknown library endpoint.")
		return
	}
	if id != "" {
		switch parts[0] {
		case "tracks":
			method, key = "getSong", "song"
		case "albums":
			method, key = "getAlbum", "album"
		case "artists":
			method, key = "getArtist", "artist"
		case "playlists":
			method, key = "getPlaylist", "playlist"
		}
		out, e := a.s.call(r.Context(), se, method, url.Values{"id": {id}})
		if e != nil {
			verror(w, 502, "upstream_unavailable", "Library request failed.")
			return
		}
		o := object(out[key])
		if o["id"] == nil {
			verror(w, 404, "not_found", "Library item not found.")
			return
		}
		if parts[0] == "playlists" {
			p := playlistDTO(o, se)
			w.Header().Set("ETag", playlistETag(o))
			dataOut(w, 200, p)
			return
		}
		n := normalize(kind, o)
		if kind == "track" {
			dataOut(w, 200, n)
		} else {
			children := []any{}
			ck := "song"
			childKind := "track"
			label := "tracks"
			if kind == "artist" {
				ck = "album"
				childKind = "album"
				label = "albums"
			}
			for _, row := range items(o, ck) {
				children = append(children, normalize(childKind, row))
			}
			dataOut(w, 200, map[string]any{kind: n, label: children})
		}
		return
	}
	rows := []any{}
	q := url.Values{}
	offset := 0
	for {
		q.Set("offset", strconv.Itoa(offset))
		q.Set("size", "200")
		q.Set("type", "alphabeticalByName")
		if sort := r.URL.Query().Get("sort"); sort == "newest" {
			q.Set("type", sort)
		}
		if method == "search3" {
			q.Set("query", r.URL.Query().Get("query"))
			q.Set("songCount", "200")
			q.Set("songOffset", strconv.Itoa(offset))
			q.Set("albumCount", "200")
			q.Set("albumOffset", strconv.Itoa(offset))
			q.Set("artistCount", "200")
			q.Set("artistOffset", strconv.Itoa(offset))
		}
		out, e := a.s.call(r.Context(), se, method, q)
		if e != nil {
			verror(w, 502, "upstream_unavailable", "Library request failed.")
			return
		}
		o := object(out[key])
		batch := []map[string]any{}
		switch parts[0] {
		case "tracks":
			batch = items(o, "song")
		case "albums":
			batch = items(o, "album")
		case "artists":
			for _, idx := range items(o, "index") {
				batch = append(batch, items(idx, "artist")...)
			}
		case "playlists":
			batch = items(o, "playlist")
		case "favorites":
			for _, k := range []string{"song", "album", "artist"} {
				label := k
				if k == "song" {
					label = "track"
				}
				for _, row := range items(o, k) {
					rows = append(rows, map[string]any{"kind": label, "id": row["id"]})
				}
			}
		case "search":
			scope := r.URL.Query().Get("scope")
			if scope == "playlist" {
				out, e := a.s.call(r.Context(), se, "getPlaylists", nil)
				if e != nil {
					verror(w, 502, "upstream_unavailable", "Playlist search failed.")
					return
				}
				for _, row := range items(object(out["playlists"]), "playlist") {
					if strings.Contains(strings.ToLower(textValue(row["name"])), strings.ToLower(r.URL.Query().Get("query"))) {
						rows = append(rows, playlistDTO(row, se))
					}
				}
				a.page(w, r, se, rows)
				return
			}
			kind = scope
			ck := scope
			if ck == "track" {
				ck = "song"
			}
			if ck != "song" && ck != "album" && ck != "artist" {
				verror(w, 422, "validation_failed", "Choose search scope track, album, artist or playlist.")
				return
			}
			batch = items(o, ck)
		}
		for _, row := range batch {
			if parts[0] == "playlists" {
				p := playlistDTO(row, se)
				delete(p, "entries")
				rows = append(rows, p)
			} else {
				rows = append(rows, normalize(kind, row))
			}
		}
		if len(batch) < 200 || method == "getArtists" || method == "getPlaylists" || method == "getStarred2" {
			break
		}
		offset += len(batch)
		if offset > 100000 {
			verror(w, 422, "unsupported", "Library scan exceeds the installation limit.")
			return
		}
	}
	if parts[0] != "albums" || r.URL.Query().Get("sort") != "newest" {
		sort.SliceStable(rows, func(i, j int) bool {
			l, _ := json.Marshal(rows[i])
			rr, _ := json.Marshal(rows[j])
			return string(l) < string(rr)
		})
	}
	a.page(w, r, se, rows)
}
func playlistETag(o map[string]any) string {
	b, _ := json.Marshal(o)
	return `"p` + digest(string(b)) + `"`
}
func playlistDTO(o map[string]any, se *session) map[string]any {
	entries := []any{}
	counts := map[string]int{}
	for _, t := range items(o, "entry") {
		id := textValue(t["id"])
		n := counts[id]
		counts[id]++
		entries = append(entries, map[string]any{"entryId": digest(textValue(o["id"]) + id + strconv.Itoa(n))[:24], "track": normalize("track", t)})
	}
	return map[string]any{"id": o["id"], "name": o["name"], "owner": o["owner"], "canEdit": o["owner"] == se.NavUser, "version": strings.Trim(playlistETag(o), `"`), "entries": entries}
}
