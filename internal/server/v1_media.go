package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type mediaLease struct {
	ID, Hash, User, Device, BrowserHash, Library, Kind, Resource, Quality, PlaybackID string
	Expires                                                                           time.Time
}
type assetRecord struct {
	ID       string `json:"id"`
	Session  string `json:"sessionId"`
	Path     string `json:"path"`
	Bytes    int64  `json:"byteLength"`
	Hash     string `json:"sha256"`
	Duration int64  `json:"durationMs"`
}

func (a *apiV1) lease(w http.ResponseWriter, r *http.Request, se *session) {
	if r.Method != "POST" {
		verror(w, 405, "unsupported", "Use POST.")
		return
	}
	var in struct {
		Kind    string `json:"kind"`
		ID      string `json:"resourceId"`
		Quality string `json:"quality"`
	}
	if decode(r, &in) != nil || in.ID == "" || len(in.ID) > 256 {
		verror(w, 422, "validation_failed", "Media kind and resourceId required.")
		return
	}
	if in.Quality != "" && in.Quality != "original" {
		verror(w, 422, "unsupported", "Only original library quality is available.")
		return
	}
	ct := ""
	var size, hash any
	ranges := false
	a.mu.Lock()
	defer a.mu.Unlock()
	switch in.Kind {
	case "recordingSegment":
		var ar assetRecord
		if a.get("asset", in.ID, accountID(se.User), &ar) != nil {
			verror(w, 404, "not_found", "Recording asset not found.")
			return
		}
		var job recordingJob
		if a.get("job", ar.Session, accountID(se.User), &job) != nil || job.Tombstone {
			verror(w, 410, "asset_expired", "Recording was removed.")
			return
		}
		ct = "audio/mp4"
		size = ar.Bytes
		hash = ar.Hash
		ranges = true
	case "radioLive":
		c, e := a.collection(se)
		found := false
		if e == nil {
			for _, s := range c.Stations {
				if s.ID == in.ID {
					found = true
				}
			}
		}
		if !found {
			verror(w, 404, "not_found", "Station not found.")
			return
		}
		ct = "audio/mpeg"
	case "trackStream", "trackOriginal", "cover":
		if se.ServerURL == "" {
			verror(w, 503, "upstream_unavailable", "Library connection is unavailable.")
			return
		}
		if in.Kind != "cover" {
			out, e := a.s.call(r.Context(), se, "getSong", url.Values{"id": {in.ID}})
			if e != nil || len(out["song"]) == 0 {
				verror(w, 404, "not_found", "Track not available to this account.")
				return
			}
			ct = textValue(object(out["song"])["contentType"])
		} else {
			ct = "image/*"
		}
		ranges = true
	default:
		verror(w, 422, "unsupported", "Unsupported media kind.")
		return
	}
	token := randomID()
	ml := mediaLease{ID: randomID(), Hash: digest(token), User: se.User, Device: se.DeviceID, Library: a.libraryID(se), Kind: in.Kind, Resource: in.ID, Quality: in.Quality, Expires: a.now().Add(12 * time.Hour)}
	if se.DeviceID == "" {
		cookie, e := r.Cookie("weazl_session")
		if e != nil {
			verror(w, 401, "unauthorized", "Media requires a session.")
			return
		}
		ml.BrowserHash = digest(cookie.Value)
		if ml.Expires.After(se.Expires) {
			ml.Expires = se.Expires
		}
	}
	if in.Kind == "radioLive" {
		ml.PlaybackID = randomID()[:24]
	}
	if a.put("lease", ml.ID, accountID(se.User), ml) != nil {
		verror(w, 503, "storage_full", "Could not save media lease.")
		return
	}
	// Relative URLs work behind home and remote origins without trusting forwarded Host headers.
	dataOut(w, 200, map[string]any{"url": "/api/v1/media/assets/" + ml.ID + "?lease=" + token, "expiresAt": ml.Expires, "contentType": ct, "byteLength": size, "sha256": hash, "supportsRanges": ranges, "playbackId": func() any {
		if ml.PlaybackID != "" {
			return ml.PlaybackID
		}
		return nil
	}()})
}
func (a *apiV1) leaseIdentity(ml mediaLease) (*session, bool) {
	if ml.Device != "" {
		var d deviceSession
		if a.get("device", ml.Device, accountID(ml.User), &d) != nil || d.Revoked || !a.now().Before(d.RefreshExpires) {
			return nil, false
		}
		se, e := a.deviceIdentity(d)
		if e != nil || a.libraryID(se) != ml.Library {
			return nil, false
		}
		se.DeviceID = d.ID
		se.Expires = ml.Expires
		return se, true
	}
	a.s.mu.Lock()
	se := a.s.sessions[ml.BrowserHash]
	a.s.mu.Unlock()
	if se == nil || !a.now().Before(se.Expires) || se.ctx.Err() != nil {
		return nil, false
	}
	clone := *se
	return &clone, true
}
func (a *apiV1) asset(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	var ml mediaLease
	err := a.get("lease", r.PathValue("id"), "*", &ml)
	var se *session
	var valid bool
	if err == nil {
		se, valid = a.leaseIdentity(ml)
	}
	a.mu.Unlock()
	if err != nil || ml.Hash != digest(r.URL.Query().Get("lease")) || !valid {
		verror(w, 401, "unauthorized", "Media authorization is invalid or revoked.")
		return
	}
	if !a.now().Before(ml.Expires) {
		verror(w, 410, "asset_expired", "Media lease expired; request a new lease.")
		return
	}
	if ml.Kind == "recordingSegment" {
		a.mu.Lock()
		var ar assetRecord
		var job recordingJob
		err = a.get("asset", ml.Resource, accountID(ml.User), &ar)
		if err == nil {
			err = a.get("job", ar.Session, accountID(ml.User), &job)
		}
		// Open under the deletion lock: unlink cannot damage this already-open transfer.
		var f *os.File
		if err == nil && !job.Tombstone {
			f, err = os.Open(filepath.Join(a.s.cfg.DataDir, "recordings", ar.Path))
		}
		a.mu.Unlock()
		if err != nil || job.Tombstone {
			verror(w, 410, "asset_expired", "Recording asset was removed.")
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "audio/mp4")
		w.Header().Set("ETag", `"`+ar.Hash+`"`)
		w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
		http.ServeContent(w, r, ar.ID+".m4a", time.Time{}, f)
		return
	}
	if ml.Kind == "radioLive" {
		if r.Method == "HEAD" {
			verror(w, 405, "unsupported", "Live audio has no static HEAD representation.")
			return
		}
		a.mu.Lock()
		c, e := a.collection(se)
		a.mu.Unlock()
		raw := ""
		if e == nil {
			for _, s := range c.Stations {
				if s.ID == ml.Resource {
					raw = s.URL
				}
			}
		}
		if raw == "" {
			verror(w, 404, "not_found", "Station was removed.")
			return
		}
		q := url.Values{"url": {raw}, "playback": {ml.PlaybackID}}
		r.URL.RawQuery = q.Encode()
		se.radio = a.radioHub(ml.Device, ml.BrowserHash)
		a.s.radioStream(w, r, se)
		return
	}
	method := "stream"
	if ml.Kind == "trackOriginal" {
		method = "download"
	}
	if ml.Kind == "cover" {
		method = "getCoverArt"
	}
	ctx, cancel := context.WithDeadline(r.Context(), ml.Expires)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, r.Method, a.s.endpoint(se, method, url.Values{"id": {ml.Resource}}), nil)
	if e != nil {
		verror(w, 502, "upstream_unavailable", "Invalid media resource.")
		return
	}
	for _, k := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		req.Header.Set(k, r.Header.Get(k))
	}
	res, e := a.s.upstream.Do(req)
	if e != nil {
		verror(w, 502, "upstream_unavailable", "Media is unavailable.")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 206 && res.StatusCode != 416 && res.StatusCode != 304 {
		verror(w, 502, "upstream_rejected", "Upstream rejected this media resource.")
		return
	}
	ct := res.Header.Get("Content-Type")
	if strings.Contains(ct, "json") || strings.Contains(ct, "xml") && ml.Kind != "cover" || strings.Contains(ct, "html") {
		verror(w, 502, "upstream_rejected", "Upstream returned an invalid media representation.")
		return
	}
	for _, k := range []string{"ETag", "Last-Modified"} {
		if v := res.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	copyMedia(w, res)
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
	dataOut(w, 200, map[string]any{"contractRevision": contractRevision, "library": se.ServerURL != "", "originalDownloads": true, "favorites": []string{"track", "album", "artist"}, "playlistReplace": true, "maxPlaylistTracks": 1000, "scrobble": true, "radio": true, "radioDirectory": []string{"somafm", "icecast"}, "mood": true, "flightRecorder": map[string]any{"enabled": a.mediaEnabled, "maxStations": 6, "defaultDurationMs": 14400000, "maxDurationMs": 43200000, "maxConcurrentStreams": 6, "recurrence": []string{"once", "daily", "weekly"}, "inputFormats": []string{"mp3", "aac"}, "outputProfiles": []string{"aac-lc-m4a"}, "outputEncoding": map[string]any{"encoder": "fdkaac", "profile": "AAC-LC", "bitRateKbps": recorderBitrateKbps, "sampleRateHz": 44100, "channels": 2}, "overnightValidated": false, "appleValidated": false}})
}

var _ = json.Marshal
