package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

type moodTrack struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	AlbumID  string `json:"albumId"`
	CoverArt string `json:"coverArt"`
	Duration int    `json:"duration"`
	Genre    string `json:"genre,omitempty"`
	Year     int    `json:"year,omitempty"`
}
type moodPlaylist struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Owner     string `json:"owner"`
	SongCount int    `json:"songCount"`
}

func (s *Server) moodCandidates(ctx context.Context, se *session, seed moodTrack) ([]moodTrack, error) {
	rows := []moodTrack{seed}
	seen := map[string]bool{seed.ID: true}
	add := func(raw json.RawMessage) {
		var body struct {
			Song []moodTrack `json:"song"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return
		}
		for _, t := range body.Song {
			if t.ID != "" && !seen[t.ID] && len(rows) < 240 {
				rows = append(rows, t)
				seen[t.ID] = true
			}
		}
	}
	if out, err := s.call(ctx, se, "getSimilarSongs2", url.Values{"id": {seed.ID}, "count": {"100"}}); err == nil {
		add(out["similarSongs2"])
	}
	q := url.Values{"size": {"200"}}
	if seed.Genre != "" {
		q.Set("genre", seed.Genre)
	}
	if out, err := s.call(ctx, se, "getRandomSongs", q); err == nil {
		add(out["randomSongs"])
	}
	if len(rows) < 100 {
		if out, err := s.call(ctx, se, "getRandomSongs", url.Values{"size": {"240"}}); err == nil {
			add(out["randomSongs"])
		}
	}
	if len(rows) < 20 {
		return nil, errors.New("Navidrome returned fewer than 20 eligible tracks for Mood")
	}
	return rows, nil
}
func (s *Server) startMoodPlaylist(ctx context.Context, se *session, seed moodTrack) (moodPlaylist, error) {
	out, err := s.call(ctx, se, "getPlaylists", nil)
	if err != nil {
		return moodPlaylist{}, err
	}
	var list struct {
		Playlist []moodPlaylist `json:"playlist"`
	}
	if json.Unmarshal(out["playlists"], &list) != nil {
		return moodPlaylist{}, errors.New("Invalid playlist response")
	}
	q := url.Values{"name": {"Mood"}, "songId": {seed.ID}}
	existing := moodPlaylist{}
	for _, p := range list.Playlist {
		if p.Owner == se.NavUser && strings.EqualFold(p.Name, "Mood") {
			if existing.ID != "" {
				return moodPlaylist{}, errors.New("Multiple owned Mood playlists exist; rename duplicates first")
			}
			existing = p
		}
	}
	if existing.ID != "" {
		q.Set("playlistId", existing.ID)
	}
	out, err = s.call(ctx, se, "createPlaylist", q)
	if err != nil {
		return moodPlaylist{}, err
	}
	var p moodPlaylist
	if json.Unmarshal(out["playlist"], &p) != nil || p.ID == "" {
		if existing.ID != "" {
			p = existing
		} else {
			return p, errors.New("Navidrome did not return the Mood playlist")
		}
	}
	p.Name = "Mood"
	p.Owner = se.NavUser
	p.SongCount = 1
	return p, nil
}
func moodKey(se *session) string { return se.ServerURL + "/" + se.NavUser }
func (s *Server) moodBusy(se *session, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	active, ok := s.moodJobs[moodKey(se)]
	return ok && id != "" && (active == id || strings.EqualFold(id, "Mood"))
}
