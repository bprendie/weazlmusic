package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type playlistInput struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	SongIDs []string `json:"songIds"`
	Remove  []int    `json:"remove"`
}

func (s *Server) owned(ctx context.Context, se *session, id string) (int, error) {
	out, err := s.call(ctx, se, "getPlaylist", url.Values{"id": {id}})
	if err != nil {
		return 0, err
	}
	var p struct {
		Owner     string `json:"owner"`
		SongCount int    `json:"songCount"`
	}
	if json.Unmarshal(out["playlist"], &p) != nil || p.Owner != se.NavUser {
		return 0, errors.New("Only the playlist owner can edit this playlist")
	}
	return p.SongCount, nil
}
func (s *Server) playlistWrite(w http.ResponseWriter, r *http.Request, se *session) {
	var in playlistInput
	if decode(r, &in) != nil || len(in.SongIDs) > 1000 || len(in.Remove) > 1000 || len(in.Name) > 200 {
		fail(w, 400, "Invalid playlist request (maximum 1,000 tracks per operation)")
		return
	}
	if s.moodBusy(se, in.ID) || s.moodBusy(se, in.Name) {
		fail(w, 409, "Stop the active Mood run before editing this playlist")
		return
	}
	q := url.Values{}
	method := "createPlaylist"
	if in.ID == "" {
		if strings.TrimSpace(in.Name) == "" || len(in.Remove) > 0 {
			fail(w, 400, "A playlist name is required")
			return
		}
		q.Set("name", strings.TrimSpace(in.Name))
		for _, id := range in.SongIDs {
			q.Add("songId", id)
		}
	} else {
		count, err := s.owned(r.Context(), se, in.ID)
		if err != nil {
			fail(w, 403, err.Error())
			return
		}
		method = "updatePlaylist"
		q.Set("playlistId", in.ID)
		if strings.TrimSpace(in.Name) != "" {
			q.Set("name", strings.TrimSpace(in.Name))
		}
		for _, id := range in.SongIDs {
			q.Add("songIdToAdd", id)
		}
		for _, n := range in.Remove {
			if n < 0 || n >= count {
				fail(w, 400, "Track position is no longer valid. Refresh the playlist.")
				return
			}
			q.Add("songIndexToRemove", strconv.Itoa(n))
		}
	}
	out, err := s.call(r.Context(), se, method, q)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	jsonOut(w, 200, out)
}
func (s *Server) playlistDelete(w http.ResponseWriter, r *http.Request, se *session) {
	var in struct {
		ID string `json:"id"`
	}
	if decode(r, &in) != nil || in.ID == "" {
		fail(w, 400, "Playlist ID required")
		return
	}
	if s.moodBusy(se, in.ID) {
		fail(w, 409, "Stop the active Mood run before deleting it")
		return
	}
	if _, err := s.owned(r.Context(), se, in.ID); err != nil {
		fail(w, 403, err.Error())
		return
	}
	if _, err := s.call(r.Context(), se, "deletePlaylist", url.Values{"id": {in.ID}}); err != nil {
		fail(w, 502, err.Error())
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
