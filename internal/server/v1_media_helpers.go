package server

import (
	"net/http"
	"time"
)

type mediaLease struct {
	ID, Hash, User, Device, BrowserHash, Library, Kind, Resource, Quality, PlaybackID, Representation string
	Expires                                                                                           time.Time
}
type assetRecord struct {
	ID       string `json:"id"`
	Session  string `json:"sessionId"`
	Path     string `json:"path"`
	Bytes    int64  `json:"byteLength"`
	Hash     string `json:"sha256"`
	Duration int64  `json:"durationMs"`
}

func (a *apiV1) radioHub(device, browser string) *radioHub {
	key := device + browser
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	if a.s.nativeRadio == nil {
		a.s.nativeRadio = map[string]*radioHub{}
	}
	if a.s.nativeRadio[key] == nil {
		a.s.nativeRadio[key] = newRadioHub()
	}
	return a.s.nativeRadio[key]
}
func (a *apiV1) radioEvents(w http.ResponseWriter, r *http.Request, se *session) {
	clone := *se
	se = &clone
	browser := ""
	if c, e := r.Cookie("weazl_session"); e == nil {
		browser = digest(c.Value)
	}
	se.radio = a.radioHub(se.DeviceID, browser)
	a.s.radioEvents(w, r, se)
}
func (a *apiV1) capabilities(w http.ResponseWriter, se *session) {
	dataOut(w, 200, map[string]any{"weazlpod": a.podCapabilities(), "contractRevision": contractRevision, "library": se.ServerURL != "", "originalDownloads": true, "favorites": []string{"track", "album", "artist"}, "playlistReplace": true, "maxPlaylistTracks": 1000, "scrobble": true, "radio": true, "radioDirectory": []string{"somafm", "icecast"}, "mood": true, "flightRecorder": map[string]any{"enabled": a.mediaEnabled, "maxStations": 6, "defaultDurationMs": 14400000, "maxDurationMs": 43200000, "maxConcurrentStreams": 6, "recurrence": []string{"once", "daily", "weekly"}, "inputFormats": []string{"mp3", "aac"}, "outputProfiles": []string{"aac-lc-m4a"}, "outputEncoding": map[string]any{"encoder": "fdkaac", "profile": "AAC-LC", "bitRateKbps": recorderBitrateKbps, "sampleRateHz": 44100, "channels": 2}, "overnightValidated": false, "appleValidated": false}})
}
