package server

import (
	"net/http"
	"os"
	"time"
)

func podEncoding() map[string]any {
	return map[string]any{"profile": podProfile, "codec": "mp3", "bitRateKbps": 128, "bitRateMode": "cbr", "sampleRateHz": 44100, "channels": 2}
}
func (a *apiV1) podLease(w http.ResponseWriter, r *http.Request, se *session, j podPreparation) {
	token := randomID()
	ml := mediaLease{ID: randomID(), Hash: digest(token), User: se.User, Device: se.DeviceID, Library: a.libraryID(se), Kind: j.Kind, Resource: j.Resource, Quality: podProfile, Representation: j.Representation, Expires: a.now().Add(12 * time.Hour)}
	if se.DeviceID == "" {
		cookie, err := r.Cookie("weazl_session")
		if err != nil {
			verror(w, 401, "unauthorized", "Media requires a session.")
			return
		}
		ml.BrowserHash = digest(cookie.Value)
		if ml.Expires.After(se.Expires) {
			ml.Expires = se.Expires
		}
	}
	p := a.pod
	p.mu.Lock()
	rep, ok := a.podPinLocked(j.Representation, ml.Expires)
	if !ok {
		p.mu.Unlock()
		verror(w, 410, "asset_expired", "Representation expired; prepare again.")
		return
	}
	err := a.put("lease", ml.ID, accountID(se.User), ml)
	p.mu.Unlock()
	if err != nil {
		verror(w, 503, "storage_full", "Could not save media lease.")
		return
	}
	dataOut(w, 200, podReadyData(j, ml, rep, token))
}
func podReadyData(j podPreparation, ml mediaLease, rep podRepresentation, token string) map[string]any {
	data := podEncoding()
	data["state"] = "ready"
	data["preparationId"] = j.ID
	data["url"] = "/api/v1/media/assets/" + ml.ID + "?lease=" + token
	data["expiresAt"] = ml.Expires
	data["representationId"] = rep.ID
	data["sourceSha256"] = rep.SourceHash
	data["byteLength"] = rep.Bytes
	data["sha256"] = rep.Hash
	data["durationMs"] = rep.Duration
	data["decodedSamples"] = rep.Samples
	data["encoderDelaySamples"] = rep.Delay
	data["encoderPaddingSamples"] = rep.Padding
	data["decoderSkipSamples"] = rep.DecoderSkip
	data["decoderDiscardSamples"] = rep.DecoderDiscard
	data["referenceDecoderDelaySamples"] = rep.DecoderDelay
	data["decoderTrimBasis"] = "ffmpeg"
	data["contentType"] = "audio/mpeg"
	data["supportsRanges"] = true
	data["playbackId"] = nil
	data["mediaStartMs"] = 0
	data["seekOrigin"] = "gapless-decoded-sample-zero"
	data["requiresGaplessTrimming"] = true
	return data
}
func (a *apiV1) podAsset(w http.ResponseWriter, r *http.Request, se *session, ml mediaLease) {
	if !a.podAuthorize(r.Context(), se, ml.Kind, ml.Resource) {
		verror(w, 404, "not_found", "Source is no longer available to this account.")
		return
	}
	p := a.pod
	p.mu.Lock()
	rep, ok := p.reps[ml.Representation]
	var f *os.File
	var err error
	if ok {
		f, err = os.Open(p.path(rep.ID))
		if err == nil {
			p.readers[rep.ID]++
		}
	}
	p.mu.Unlock()
	if !ok || err != nil {
		verror(w, 410, "asset_expired", "Representation expired; prepare again.")
		return
	}
	defer func() { f.Close(); p.mu.Lock(); p.readers[rep.ID]--; p.mu.Unlock() }()
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("ETag", `"`+rep.Hash+`"`)
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	http.ServeContent(w, r, rep.ID+".mp3", time.Time{}, f)
}
func (a *apiV1) podCapabilities() map[string]any {
	enabled := a.pod.encoder != ""
	profiles := []any{}
	if enabled {
		profiles = append(profiles, podEncoding())
	}
	return map[string]any{"extensionRevision": "2026-10-20.1", "identificationHeader": "X-Weazl-Client", "client": "weazlpod", "profiles": profiles, "finiteLibrary": enabled, "trackDownload": enabled, "recordingSegments": enabled, "radioLive": enabled, "preparations": true, "maxWorkers": a.podWorkers(), "maxLiveWorkers": 2, "maxLiveWorkersPerAccount": 1, "maxInputBytes": podInputLimit, "maxOutputBytes": podOutputLimit, "maxDurationMs": 14400000, "budgetBytes": a.podBudget(), "gaplessTrimmingRequired": true}
}
