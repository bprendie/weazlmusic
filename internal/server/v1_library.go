package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type librarySnapshot struct {
	Owner, Library, Route string
	Rows                  []any
	Expires               time.Time
}

func object(raw json.RawMessage) map[string]any {
	var o map[string]any
	_ = json.Unmarshal(raw, &o)
	if o == nil {
		o = map[string]any{}
	}
	return o
}
func items(o map[string]any, key string) []map[string]any {
	out := []map[string]any{}
	for _, v := range array(o[key]) {
		if row, ok := v.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}
func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return []any{}
}
func number(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
func textValue(v any) string { s, _ := v.(string); return s }
func starred(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return textValue(v) != ""
}
func normalize(kind string, o map[string]any) map[string]any {
	n := map[string]any{"id": o["id"], "starred": starred(o["starred"])}
	switch kind {
	case "track":
		for _, k := range []string{"title", "artist", "album", "artistId", "albumId", "contentType"} {
			n[k] = o[k]
		}
		n["durationMs"] = number(o["duration"]) * 1000
		n["discNumber"] = o["discNumber"]
		n["trackNumber"] = o["track"]
		n["bitRateKbps"] = o["bitRate"]
		n["mediaRevision"] = nil
	case "album":
		for _, k := range []string{"name", "artist", "artistId", "year", "genre", "songCount"} {
			n[k] = o[k]
		}
		n["durationMs"] = number(o["duration"]) * 1000
		n["addedAt"] = o["created"]
	case "artist":
		n["name"] = o["name"]
		n["albumCount"] = o["albumCount"]
	}
	n["coverId"] = o["coverArt"]
	return n
}
func (a *apiV1) page(w http.ResponseWriter, r *http.Request, se *session, rows []any) {
	limit := 100
	var e error
	if q := r.URL.Query().Get("limit"); q != "" {
		limit, e = strconv.Atoi(q)
	}
	if e != nil || limit < 1 || limit > 200 {
		verror(w, 422, "validation_failed", "Page limit must be 1–200.")
		return
	}
	route := r.URL.Path + "?" + r.URL.Query().Get("query") + ":" + r.URL.Query().Get("scope") + ":" + r.URL.Query().Get("sort") + ":" + r.URL.Query().Get("provider") + ":" + r.URL.Query().Get("state")
	a.mu.Lock()
	defer a.mu.Unlock()
	id := ""
	offset := 0
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(cursor)
		parts := strings.Split(string(b), ":")
		if err != nil || len(parts) != 2 {
			verror(w, 422, "validation_failed", "Invalid cursor.")
			return
		}
		id = parts[0]
		offset, err = strconv.Atoi(parts[1])
		snap := a.snapshot[id]
		if err != nil || offset < 0 || snap == nil || snap.Owner != accountID(se.User) || snap.Library != a.libraryID(se) || snap.Route != route || !a.now().Before(snap.Expires) {
			verror(w, 409, "snapshot_expired", "The list snapshot expired; start again.")
			return
		}
		rows = snap.Rows
	} else {
		for k, s := range a.snapshot {
			if !a.now().Before(s.Expires) {
				delete(a.snapshot, k)
			}
		}
		if len(a.snapshot) >= 32 {
			var oldest string
			var t time.Time
			for k, s := range a.snapshot {
				if oldest == "" || s.Expires.Before(t) {
					oldest = k
					t = s.Expires
				}
			}
			delete(a.snapshot, oldest)
		}
		id = randomID()
		a.snapshot[id] = &librarySnapshot{accountID(se.User), a.libraryID(se), route, rows, a.now().Add(5 * time.Minute)}
	}
	if offset > len(rows) {
		verror(w, 409, "snapshot_expired", "Invalid snapshot position.")
		return
	}
	end := min(offset+limit, len(rows))
	var next any
	if end < len(rows) {
		next = base64.RawURLEncoding.EncodeToString([]byte(id + ":" + strconv.Itoa(end)))
	}
	jsonOut(w, 200, map[string]any{"data": rows[offset:end], "nextCursor": next, "snapshotRevision": id})
}
