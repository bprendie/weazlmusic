package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func playlistTarget(body, base string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "file") {
			if _, v, ok := strings.Cut(line, "="); ok {
				line = strings.TrimSpace(v)
			}
		} else if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") || strings.Contains(line, "=") {
			continue
		}
		if line == "" {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			continue
		}
		b, err := url.Parse(base)
		if err != nil {
			return "", err
		}
		u = b.ResolveReference(u)
		if validRadioURL(u.String()) == nil {
			return u.String(), nil
		}
	}
	return "", errors.New("Playlist contains no playable stream")
}
func (s *Server) resolveRadio(ctx context.Context, raw string) (*http.Response, error) {
	for depth := 0; depth < 4; depth++ {
		if err := validRadioURL(raw); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Icy-MetaData", "1")
		req.Header.Set("User-Agent", "WeazlTunes/1.0")
		res, err := s.radio.Do(req)
		if err != nil {
			return nil, errors.New("Could not reach radio stream")
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return nil, errors.New("Radio server is unavailable")
		}
		ct := strings.ToLower(res.Header.Get("Content-Type"))
		path := strings.ToLower(res.Request.URL.Path)
		if strings.HasSuffix(path, ".pls") || strings.HasSuffix(path, ".m3u") || strings.Contains(ct, "scpls") || strings.Contains(ct, "mpegurl") {
			b, err := io.ReadAll(io.LimitReader(res.Body, 65537))
			res.Body.Close()
			if err != nil || len(b) > 65536 {
				return nil, errors.New("Radio playlist is invalid or too large")
			}
			if strings.Contains(string(b), "#EXT-X-") {
				return nil, errors.New("HLS radio is not supported yet; use a direct MP3/AAC stream")
			}
			raw, err = playlistTarget(string(b), res.Request.URL.String())
			if err != nil {
				return nil, err
			}
			continue
		}
		if strings.Contains(ct, "text/") || strings.Contains(ct, "json") || strings.Contains(ct, "xml") {
			res.Body.Close()
			return nil, errors.New("This URL is not an audio stream")
		}
		return res, nil
	}
	return nil, errors.New("Too many nested radio playlists")
}
func (s *Server) radioStream(w http.ResponseWriter, r *http.Request, se *session) {
	raw := r.URL.Query().Get("url")
	if len(raw) > 2048 {
		fail(w, 400, "Stream URL is too long")
		return
	}
	publish := func(radioMetadata) {}
	if id := r.URL.Query().Get("playback"); id != "" {
		if !playbackID.MatchString(id) {
			fail(w, 400, "Invalid playback ID")
			return
		}
		feed, release, err := se.radio.acquire(id)
		if err != nil {
			fail(w, 429, err.Error())
			return
		}
		defer release()
		if !feed.begin() {
			fail(w, 409, "This radio playback is already active")
			return
		}
		publish = feed.publish
		defer func() { feed.publish(radioMetadata{Ended: true}) }()
	}
	ctx, cancel := context.WithDeadline(r.Context(), se.Expires)
	defer cancel()
	stop := context.AfterFunc(se.ctx, cancel)
	defer stop()
	res, err := s.resolveRadio(ctx, raw)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	defer res.Body.Close()
	w.Header().Set("X-Accel-Buffering", "no")
	intervalHeader := res.Header.Get("Icy-Metaint")
	if intervalHeader == "" {
		copyMedia(w, res)
		return
	}
	interval, err := strconv.ParseInt(intervalHeader, 10, 64)
	if err != nil || interval <= 0 || interval > 1<<20 {
		fail(w, 502, "Invalid radio metadata interval")
		return
	}
	station := metadataText([]byte(res.Header.Get("Icy-Name")))
	publish(radioMetadata{Station: station})
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.WriteHeader(http.StatusOK)
	flush := func() { _ = http.NewResponseController(w).Flush() }
	flush()
	_ = relayICY(w, res.Body, interval, func(meta radioMetadata) { meta.Station = station; publish(meta) }, flush)
}
