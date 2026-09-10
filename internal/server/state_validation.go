package server

import (
	"bytes"
	"encoding/json"
	"math"
)

func validSavedTrack(raw json.RawMessage, optional bool) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return optional
	}
	var t struct {
		ID       string   `json:"id"`
		Title    string   `json:"title"`
		Artist   string   `json:"artist"`
		Album    string   `json:"album"`
		CoverArt string   `json:"coverArt"`
		URL      string   `json:"url"`
		Duration *float64 `json:"duration"`
		Position float64  `json:"position"`
	}
	if json.Unmarshal(raw, &t) != nil || t.ID == "" || len(t.ID) > 1024 || len(t.Title) > 2000 || len(t.Artist) > 2000 || len(t.Album) > 2000 || len(t.CoverArt) > 1024 {
		return false
	}
	if t.URL != "" && validRadioURL(t.URL) != nil {
		return false
	}
	if t.Position < 0 || math.IsNaN(t.Position) || math.IsInf(t.Position, 0) {
		return false
	}
	return t.Duration == nil || (*t.Duration >= 0 && !math.IsNaN(*t.Duration) && !math.IsInf(*t.Duration, 0))
}
