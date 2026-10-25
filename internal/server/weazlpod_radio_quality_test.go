package server

import (
	"net/http/httptest"
	"testing"
)

func TestPodRadioQualitySelectors(t *testing.T) {
	for _, tc := range []struct {
		name, client, kind, body, want string
		headers                        []string
		bad                            bool
	}{
		{"legacy", "weazlpod", "radioLive", "", podProfile, nil, false},
		{"ordinary", "", "radioLive", "", "", nil, false},
		{"original", "", "radioLive", "original", "original", nil, false},
		{"low", "weazlpod", "radioLive", "", "mp3-64", []string{"low"}, false},
		{"medium", "weazlpod", "radioLive", "", "mp3-96", []string{"med"}, false},
		{"high", " WeAzLpOd ", "radioLive", "", podProfile, []string{" HIGH "}, false},
		{"matching", "weazlpod", "radioLive", "mp3-64", "mp3-64", []string{"low"}, false},
		{"explicit", "", "radioLive", "mp3-96", "mp3-96", nil, false},
		{"pod explicit", "weazlpod", "radioLive", "mp3-96", "mp3-96", nil, false},
		{"conflict", "weazlpod", "radioLive", podProfile, "", []string{"low"}, true},
		{"unknown", "weazlpod", "radioLive", "", "", []string{"ultra"}, true},
		{"empty", "weazlpod", "radioLive", "", "", []string{""}, true},
		{"duplicate", "weazlpod", "radioLive", "", "", []string{"low", "low"}, true},
		{"joined", "weazlpod", "radioLive", "", "", []string{"low,high"}, true},
		{"no identity", "", "radioLive", "", "", []string{"low"}, true},
		{"wrong identity", "other", "radioLive", "", "", []string{"low"}, true},
		{"wrong kind", "weazlpod", "trackDownload", "", "", []string{"low"}, true},
		{"cover", "weazlpod", "cover", "", "", []string{"low"}, true},
		{"pod original", "weazlpod", "radioLive", "original", "", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", nil)
			r.Header.Set("X-Weazl-Client", tc.client)
			for _, v := range tc.headers {
				r.Header.Add(podRadioQualityHeader, v)
			}
			got, err := podSelectRadioQuality(r, tc.kind, tc.body)
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("got %q / %v; want %q bad=%v", got, err, tc.want, tc.bad)
			}
		})
	}
}

func TestPodRadioQualityLeaseContract(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "radio-quality")
	code, b, _ := h.vrequest("POST", "radio/stations", map[string]any{"name": "Quality fixture", "streamURL": "https://radio.fixture.invalid/audio", "preset": false}, c.Access, nil)
	if code != 201 {
		t.Fatal(code, string(b))
	}
	station := decodeData[nativeStation](t, b)
	for _, tc := range []struct {
		kind, quality, client, header, want string
		status                              int
	}{
		{"radioLive", "mp3-64", "", "", "mp3-64", 200},
		{"radioLive", "mp3-96", "weazlpod", "", "mp3-96", 200},
		{"radioLive", "", "weazlpod", "", podProfile, 200},
		{"radioLive", "", "", "", "", 200},
		{"radioLive", "original", "", "", "original", 200},
		{"radioLive", "mp3-96", "weazlpod", "med", "mp3-96", 200},
		{"radioLive", "mp3-128", "weazlpod", "low", "", 422},
		{"radioLive", "mp3-192", "", "", "", 422},
		{"radioLive", "", "", "low", "", 422},
		{"trackStream", "mp3-64", "", "", "", 422},
		{"trackDownload", "mp3-96", "", "", "", 422},
		{"recordingSegment", "mp3-64", "", "", "", 422},
		{"cover", "mp3-64", "", "", "", 422},
		{"trackOriginal", "mp3-96", "", "", "", 422},
	} {
		headers := map[string]string{}
		if tc.client != "" {
			headers["X-Weazl-Client"] = tc.client
		}
		if tc.header != "" {
			headers[podRadioQualityHeader] = tc.header
		}
		code, b, _ := h.vrequest("POST", "media/leases", map[string]string{"kind": tc.kind, "resourceId": station.ID, "quality": tc.quality}, c.Access, headers)
		if code != tc.status {
			t.Fatalf("%+v: %d %s", tc, code, b)
		}
		if code == 200 && decodeData[map[string]any](t, b)["profile"] != tc.want {
			t.Fatalf("wrong selected profile for %+v", tc)
		}
	}
	code, b, _ = h.vrequest("GET", "capabilities", nil, c.Access, nil)
	if code != 200 {
		t.Fatal(code)
	}
	pod := decodeData[map[string]any](t, b)["weazlpod"].(map[string]any)
	if len(pod["profiles"].([]any)) != 1 || pod["radioQualityHeader"] != podRadioQualityHeader || pod["radioDefaultQuality"] != "high" {
		t.Fatal("changed finite profiles/default", pod)
	}
	profiles := pod["radioProfiles"].([]any)
	if len(profiles) != 3 {
		t.Fatal("missing radio profiles", profiles)
	}
	for i, bitrate := range []int{64, 96, 128} {
		p := profiles[i].(map[string]any)
		if p["bitRateKbps"] != float64(bitrate) || p["sampleRateHz"] != float64(44100) || p["channels"] != float64(2) || p["channelMode"] != "joint_stereo" {
			t.Fatal("incorrect advertised profile", p)
		}
	}
}

// Inspect every complete MPEG-1 Layer III frame, not merely ffprobe's estimate.
func assertPodRadioFrames(t *testing.T, b []byte, bitrate int) {
	t.Helper()
	rates := []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	frames := 0
	for len(b) >= 4 {
		if b[0] != 0xff || b[1]&0xfe != 0xfa || b[2]&12 != 0 || rates[b[2]>>4] != bitrate || b[3]>>6 != 1 {
			t.Fatalf("invalid CBR/44100/joint-stereo frame: %x", b[:4])
		}
		n := 144000*bitrate/44100 + int((b[2]>>1)&1)
		if n > len(b) {
			break
		}
		frames++
		b = b[n:]
	}
	if frames < 20 {
		t.Fatalf("too few complete verified frames: %d", frames)
	}
}
