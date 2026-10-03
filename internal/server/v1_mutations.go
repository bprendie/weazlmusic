package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (a *apiV1) musicMutation(w http.ResponseWriter, r *http.Request, se *session, p string) {
	parts := strings.Split(p, "/")
	if len(parts) == 3 && parts[0] == "favorites" && r.Method == "PUT" {
		kind, id := parts[1], parts[2]
		param := ""
		switch kind {
		case "track":
			param = "id"
		case "album":
			param = "albumId"
		case "artist":
			param = "artistId"
		}
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if param == "" || decode(r, &in) != nil {
			verror(w, 422, "validation_failed", "Invalid favorite request.")
			return
		}
		method := "unstar"
		if in.Enabled {
			method = "star"
		}
		if _, e := a.s.call(r.Context(), se, method, url.Values{param: {id}}); e != nil {
			verror(w, 502, "outcome_unknown", "Favorite outcome is unknown; refresh favorites.")
			return
		}
		out, e := a.s.call(r.Context(), se, "getStarred2", nil)
		if e != nil {
			verror(w, 502, "outcome_unknown", "Favorite was sent but could not be confirmed.")
			return
		}
		key := kind
		if key == "track" {
			key = "song"
		}
		found := false
		for _, row := range items(object(out["starred2"]), key) {
			if textValue(row["id"]) == id {
				found = true
			}
		}
		if found != in.Enabled {
			verror(w, 502, "upstream_rejected", "Favorite change was not confirmed.")
			return
		}
		dataOut(w, 200, map[string]any{"kind": kind, "id": id, "enabled": found})
		return
	}
	if p == "scrobbles" && r.Method == "POST" {
		var in struct {
			Occurrence string    `json:"occurrenceId"`
			Track      string    `json:"trackId"`
			PlayedAt   time.Time `json:"playedAt"`
		}
		if decode(r, &in) != nil || in.Occurrence == "" || len(in.Occurrence) > 200 || in.Track == "" || in.PlayedAt.IsZero() {
			verror(w, 422, "validation_failed", "Occurrence, track and playedAt are required.")
			return
		}
		id := digest(accountID(se.User) + a.libraryID(se) + in.Occurrence)
		var old struct{ Hash, Status string }
		b, _ := json.Marshal(in)
		hash := digest(string(b))
		if a.get("scrobble", id, accountID(se.User), &old) == nil {
			if old.Hash != hash {
				verror(w, 409, "version_conflict", "Occurrence was used with another track or time.")
				return
			}
			if old.Status != "accepted" {
				verror(w, 409, "outcome_unknown", "Scrobble outcome is unknown; do not resubmit.")
				return
			}
			dataOut(w, 200, map[string]string{"occurrenceId": in.Occurrence, "status": "accepted"})
			return
		}
		old.Hash, old.Status = hash, "outcome_unknown"
		if a.put("scrobble", id, accountID(se.User), old) != nil {
			verror(w, 503, "storage_full", "Could not save scrobble occurrence.")
			return
		}
		if _, e := a.s.call(r.Context(), se, "scrobble", url.Values{"id": {in.Track}, "time": {strconv.FormatInt(in.PlayedAt.UnixMilli(), 10)}, "submission": {"true"}}); e != nil {
			verror(w, 502, "outcome_unknown", "Scrobble outcome is unknown; do not resubmit.")
			return
		}
		old.Status = "accepted"
		if a.put("scrobble", id, accountID(se.User), old) != nil {
			verror(w, 503, "outcome_unknown", "Scrobble accepted but could not be saved.")
			return
		}
		dataOut(w, 200, map[string]string{"occurrenceId": in.Occurrence, "status": "accepted"})
		return
	}
	if parts[0] != "playlists" || len(parts) > 2 {
		verror(w, 404, "not_found", "Unknown music mutation.")
		return
	}
	id := ""
	if len(parts) == 2 {
		id = parts[1]
	}
	var before map[string]any
	if id != "" {
		out, e := a.s.call(r.Context(), se, "getPlaylist", url.Values{"id": {id}})
		if e != nil {
			verror(w, 502, "upstream_unavailable", "Playlist is unavailable.")
			return
		}
		before = object(out["playlist"])
		if before["owner"] != se.NavUser {
			verror(w, 403, "forbidden", "Only the playlist owner can edit it.")
			return
		}
		if a.s.moodBusy(se, id) {
			verror(w, 409, "version_conflict", "Stop Mood before editing this playlist.")
			return
		}
		if r.Header.Get("If-Match") == "" {
			verror(w, 428, "validation_failed", "Playlist ETag is required.")
			return
		}
		if r.Header.Get("If-Match") != playlistETag(before) {
			verror(w, 409, "version_conflict", "Playlist changed; refresh it.")
			return
		}
	}
	if r.Method == "DELETE" && id != "" {
		if _, e := a.s.call(r.Context(), se, "deletePlaylist", url.Values{"id": {id}}); e != nil {
			verror(w, 502, "outcome_unknown", "Delete outcome is unknown; refresh playlists.")
			return
		}
		if out, e := a.s.call(r.Context(), se, "getPlaylists", nil); e != nil {
			verror(w, 502, "outcome_unknown", "Delete could not be confirmed.")
			return
		} else {
			for _, row := range items(object(out["playlists"]), "playlist") {
				if textValue(row["id"]) == id {
					verror(w, 502, "upstream_rejected", "Playlist still exists.")
					return
				}
			}
		}
		w.WriteHeader(204)
		return
	}
	if !(id == "" && r.Method == "POST" || id != "" && r.Method == "PUT") {
		verror(w, 405, "unsupported", "Unsupported playlist method.")
		return
	}
	var in struct {
		Name string   `json:"name"`
		IDs  []string `json:"trackIds"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 || len(in.IDs) > 1000 || in.IDs == nil {
		verror(w, 422, "validation_failed", "Name and trackIds (up to 1,000) are required.")
		return
	}
	for _, tid := range in.IDs {
		if tid == "" {
			verror(w, 422, "validation_failed", "Track IDs cannot be empty.")
			return
		}
	}
	q := url.Values{"name": {in.Name}}
	for _, tid := range in.IDs {
		q.Add("songId", tid)
	}
	if id != "" {
		q.Set("playlistId", id)
	}
	out, e := a.s.call(r.Context(), se, "createPlaylist", q)
	if e != nil {
		verror(w, 502, "outcome_unknown", "Playlist write outcome is unknown; refresh playlists.")
		return
	}
	if id == "" {
		id = textValue(object(out["playlist"])["id"])
		if id == "" {
			verror(w, 502, "outcome_unknown", "Upstream did not return the created playlist ID.")
			return
		}
	}
	// Some Subsonic implementations ignore name on replacement; update explicitly.
	if _, e = a.s.call(r.Context(), se, "updatePlaylist", url.Values{"playlistId": {id}, "name": {in.Name}}); e != nil {
		verror(w, 502, "outcome_unknown", "Playlist contents changed but its name could not be confirmed.")
		return
	}
	out, e = a.s.call(r.Context(), se, "getPlaylist", url.Values{"id": {id}})
	if e != nil {
		verror(w, 502, "outcome_unknown", "Playlist write could not be confirmed.")
		return
	}
	o := object(out["playlist"])
	tracks := items(o, "entry")
	valid := o["owner"] == se.NavUser && o["name"] == in.Name && len(tracks) == len(in.IDs)
	if valid {
		for i, t := range tracks {
			if t["id"] != in.IDs[i] {
				valid = false
				break
			}
		}
	}
	if !valid {
		verror(w, 409, "version_conflict", "Upstream playlist differs from the requested order. Refresh it.")
		return
	}
	w.Header().Set("ETag", playlistETag(o))
	code := 200
	if r.Method == "POST" {
		code = 201
	}
	dataOut(w, code, playlistDTO(o, se))
}
