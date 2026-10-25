package server

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestPodLiveMP3AndAACMetadataOwnership(t *testing.T) {
	for _, codec := range []string{"mp3", "aac"} {
		for _, profile := range []struct {
			quality, name string
			bitrate       int
		}{{"low", "mp3-64", 64}, {"med", "mp3-96", 96}, {"high", "mp3-128", 128}} {
			t.Run(codec+"/"+profile.name, func(t *testing.T) {
				args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000", "-t", "3", "-ac", "1"}
				if codec == "aac" {
					args = append(args, "-c:a", "aac", "-f", "adts", "pipe:1")
				} else {
					args = append(args, "-c:a", "libmp3lame", "-b:a", "96k", "-f", "mp3", "pipe:1")
				}
				audio, err := exec.Command("ffmpeg", args...).Output()
				if err != nil {
					t.Fatal(err)
				}
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "" || r.Header.Get("X-Weazl-Client") != "" || r.Header.Get(podRadioQualityHeader) != "" {
						t.Error("credentials leaked")
					}
					w.Header().Set("Content-Type", "audio/"+codec)
					w.Header().Set("Icy-Metaint", "1024")
					w.(http.Flusher).Flush()
					// Cycle audio continuously; all metadata is stripped before the encoder.
					index := 0
					for {
						block := make([]byte, 1024)
						for i := range block {
							block[i] = audio[index%len(audio)]
							index++
						}
						if _, e := w.Write(block); e != nil {
							return
						}
						meta := []byte("StreamTitle='Fixture - Cue';")
						padding := make([]byte, 32)
						copy(padding, meta)
						if _, e := w.Write(append([]byte{2}, padding...)); e != nil {
							return
						}
						w.(http.Flusher).Flush()
						select {
						case <-r.Context().Done():
							return
						case <-time.After(time.Millisecond):
						}
					}
				}))
				defer origin.Close()
				h := setup(t)
				h.login("alice")
				c := h.native("alice", "pod")
				s := h.app.Config.Handler.(*Server)
				s.radio = &http.Client{Transport: fixtureRadioTransport{origin.Client(), origin.URL}}
				code, b, _ := h.vrequest("POST", "radio/stations", map[string]any{"name": "Pod fixture", "streamURL": "https://radio.fixture.invalid/audio", "preset": false}, c.Access, nil)
				if code != 201 {
					t.Fatal(code, string(b))
				}
				station := decodeData[nativeStation](t, b)
				code, b, _ = h.vrequest("POST", "media/leases", map[string]string{"kind": "radioLive", "resourceId": station.ID}, c.Access, map[string]string{"X-Weazl-Client": "weazlpod", podRadioQualityHeader: profile.quality})
				if code != 200 {
					t.Fatal(code, string(b))
				}
				lease := decodeData[map[string]any](t, b)
				if lease["profile"] != profile.name || lease["bitRateKbps"] != float64(profile.bitrate) || lease["sampleRateHz"] != float64(44100) || lease["channels"] != float64(2) || lease["channelMode"] != "joint_stereo" {
					t.Fatal("incorrect lease encoding", lease)
				}
				path := lease["url"].(string)
				client := &http.Client{Timeout: 10 * time.Second}
				res, err := client.Get(h.app.URL + path)
				if err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != 200 {
					raw, _ := io.ReadAll(res.Body)
					res.Body.Close()
					t.Fatal(res.StatusCode, string(raw))
				}
				if res.Header.Get("X-Weazl-Radio-Profile") != profile.name || res.Header.Get("Cache-Control") != "private, no-store" {
					t.Fatal("incorrect live headers", res.Header)
				}
				for _, header := range []string{"Content-Length", "Content-Range", "ETag", "Accept-Ranges", "Icy-Metaint"} {
					if res.Header.Get(header) != "" {
						t.Fatal("false live claim", header)
					}
				}
				capture := make([]byte, 32768)
				if _, err := io.ReadFull(res.Body, capture); err != nil {
					t.Fatal(err)
				}
				code, _, _ = podGet(t, h, "GET", path, nil)
				if code != 409 {
					t.Fatal("duplicate actual owner", code)
				}
				code, b, _ = h.vrequest("POST", "media/leases", map[string]string{"kind": "radioLive", "resourceId": station.ID, "quality": podProfile}, c.Access, nil)
				another := decodeData[map[string]any](t, b)
				if code != 200 {
					t.Fatal("second lease", code, string(b))
				}
				code, _, limits := podGet(t, h, "GET", another["url"].(string), nil)
				if code != 429 || limits.Get("Retry-After") == "" {
					t.Fatal("account live limit", code)
				}

				hub := s.v1.radioHub(c.Device, "")
				feed, release, err := hub.acquire(lease["playbackId"].(string))
				if err != nil {
					t.Fatal(err)
				}
				feed.mu.Lock()
				latest := feed.latest
				feed.mu.Unlock()
				release()
				if latest.Title != "Cue" || latest.Artist != "Fixture" {
					t.Fatal("metadata not separated", latest)
				}
				res.Body.Close()
				deadline := time.Now().Add(5 * time.Second)
				for {
					s.v1.pod.mu.Lock()
					running := s.v1.pod.live["alice"]
					s.v1.pod.mu.Unlock()
					if running == 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("disconnect leaked worker")
					}
					time.Sleep(10 * time.Millisecond)
				}
				req, _ := http.NewRequest("GET", h.app.URL+path, nil)
				req.Header.Set(podRadioQualityHeader, "low") // GET cannot renegotiate the lease.
				res, err = client.Do(req)
				if err != nil {
					t.Fatal("reconnect", err)
				}
				defer res.Body.Close()
				if res.Header.Get("X-Weazl-Radio-Profile") != profile.name {
					t.Fatal("GET changed leased profile")
				}
				assertPodRadioFrames(t, capture, profile.bitrate)
				file := filepath.Join(t.TempDir(), "live.mp3")
				_ = os.WriteFile(file, capture, 0600)
				out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_name,sample_rate,channels,bit_rate", "-of", "csv=p=0", file).Output()
				if err != nil || !bytes.Contains(out, []byte("mp3,44100,2,"+strconv.Itoa(profile.bitrate*1000))) {
					t.Fatal("incorrect live MP3", string(out), err)
				}
				code, _, _ = podGet(t, h, "GET", path, map[string]string{"Range": "bytes=0-1"})
				if code != 422 {
					t.Fatal("live range accepted", code)
				}
				start := time.Now()
				code, b, _ = h.vrequest("POST", "auth/logout", nil, c.Access, nil)
				if code != 204 {
					t.Fatal(code, string(b))
				}
				_, _ = io.Copy(io.Discard, res.Body)
				res.Body.Close()
				if time.Since(start) > 3*time.Second {
					t.Fatal("revocation did not stop live encoder promptly")
				}
				code, _, _ = podGet(t, h, "GET", path, nil)
				if code != 401 {
					t.Fatal("revoked live lease", code)
				}

			})
		}
	}
}
func TestPodRecordingDerivativeTimeline(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "pod")
	bob := h.native("bob", "other")
	a := h.app.Config.Handler.(*Server).v1
	path := filepath.Join(h.dir, "recordings", "pod-recording.m4a")
	args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=2:sample_rate=44100", "-f", "lavfi", "-i", "sine=frequency=880:duration=2:sample_rate=44100", "-f", "lavfi", "-i", "sine=frequency=1320:duration=2:sample_rate=44100", "-filter_complex", "[0:a][1:a][2:a]concat=n=3:v=0:a=1[a]", "-map", "[a]", "-c:a", "aac", "-ac", "2", path}
	if err := exec.Command("ffmpeg", args...).Run(); err != nil {
		t.Fatal(err)
	}
	n, hash, err := podHash(path)
	if err != nil {
		t.Fatal(err)
	}
	job := recordingJob{ID: "pod-flight", Username: "alice", Library: a.libraryID(&session{}), State: "partial", Duration: 20000}
	ar := assetRecord{ID: "pod-recording", Session: job.ID, Path: filepath.Base(path), Bytes: n, Hash: hash, Duration: 5900}
	// Two station timelines retain their gaps and switching offsets; AAC master identities stay fixed.
	manifest := recordingManifest{Schema: 1, Session: job.ID, Revision: "immutable", Duration: 20000, Tracks: []recordingTrack{
		{StationID: "a", Segments: []recordingSegment{{ID: "a1", AssetID: ar.ID, Start: 1000, Duration: 5900}}, Gaps: []recordingGap{{Start: 0, Duration: 1000}, {Start: 6900, Duration: 13100}}},
		{StationID: "b", Segments: []recordingSegment{{ID: "b1", AssetID: ar.ID, Start: 8000, Duration: 5900}}, Gaps: []recordingGap{{Start: 0, Duration: 8000}, {Start: 13900, Duration: 6100}}},
	}}
	if err = a.put("job", job.ID, accountID("alice"), job); err != nil {
		t.Fatal(err)
	}
	if err = a.put("asset", ar.ID, accountID("alice"), ar); err != nil {
		t.Fatal(err)
	}
	if err = a.put("manifest", job.ID, accountID("alice"), manifest); err != nil {
		t.Fatal(err)
	}
	body := map[string]string{"kind": "recordingSegment", "resourceId": ar.ID, "quality": podProfile}
	code, b, _ := h.vrequest("POST", "media/leases", body, bob.Access, nil)
	if code != 404 {
		t.Fatal("foreign recording", code, string(b))
	}
	code, b, _ = h.vrequest("POST", "media/leases", body, c.Access, nil)
	d := podPoll(t, h, c.Access, code, b)
	if d["sha256"] == hash || d["byteLength"] == float64(n) || d["encoderDelaySamples"].(float64) <= 0 || d["mediaStartMs"] != float64(0) {
		t.Fatal("derivative identity/trim", d)
	}
	_, raw, _ := podGet(t, h, "GET", d["url"].(string), nil)
	mp3 := filepath.Join(t.TempDir(), "recording.mp3")
	_ = os.WriteFile(mp3, raw, 0600)
	// Seek near start, middle and end of each segment on both station timelines.
	for _, track := range manifest.Tracks {
		seg := track.Segments[0]
		for _, point := range []struct {
			offset int64
			hz     int
		}{{50, 440}, {3000, 880}, {5800, 1320}} {
			timeline := seg.Start + point.offset
			mediaOffset := seg.MediaStart + timeline - seg.Start
			pcm, err := exec.Command("ffmpeg", "-v", "error", "-i", mp3, "-ss", strconv.FormatFloat(float64(mediaOffset)/1000, 'f', 3, 64), "-t", "0.1", "-f", "s16le", "pipe:1").Output()
			if err != nil || len(pcm) < 10000 {
				t.Fatal("timeline seek", err, len(pcm))
			}
			crossings := 0
			previous := int16(0)
			for i := 0; i+4 <= len(pcm); i += 4 {
				sample := int16(binary.LittleEndian.Uint16(pcm[i:]))
				if previous < 0 && sample >= 0 {
					crossings++
				}
				previous = sample
			}
			if delta := crossings - point.hz/10; delta < -3 || delta > 3 {
				t.Fatal("wrong cue after station switch/seek", track.StationID, timeline, crossings, point.hz)
			}
		}
	}
	var after recordingManifest
	_ = a.get("manifest", job.ID, accountID("alice"), &after)
	if after.Revision != manifest.Revision || after.Tracks[1].Segments[0].Start != 8000 || after.Tracks[0].Gaps[1].Start != 6900 {
		t.Fatal("master timeline mutated")
	}
	_, currentHash, _ := podHash(path)
	if currentHash != hash {
		t.Fatal("AAC master changed")
	}
	job.Tombstone = true
	_ = a.put("job", job.ID, accountID("alice"), job)
	code, _, _ = podGet(t, h, "GET", d["url"].(string), nil)
	if code != 404 {
		t.Fatal("deleted master derivative remained accessible", code)
	}
}
