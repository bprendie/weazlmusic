package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestMoodStreamingAndSettings(t *testing.T) {
	for _, provider := range []string{"ollama", "vllm"} {
		t.Run(provider, func(t *testing.T) {
			h := setup(t)
			alice := h.login("alice")
			bob := h.login("bob")
			code, b, _ := h.request("PUT", "/api/llm", map[string]string{"provider": provider, "url": h.upstream, "model": "fixture-model", "apiKey": "private-key"}, alice)
			if code != 200 {
				t.Fatalf("settings: %d %s", code, b)
			}
			_, b, _ = h.request("GET", "/api/llm", nil, alice)
			if strings.Contains(string(b), "private-key") || !strings.Contains(string(b), `"hasKey":true`) {
				t.Fatalf("key masking: %s", b)
			}
			_, b, _ = h.request("GET", "/api/llm", nil, bob)
			if !strings.Contains(string(b), `"provider":"off"`) {
				t.Fatalf("settings isolation: %s", b)
			}
			var previous string
			for run := 0; run < 2; run++ {
				req, _ := http.NewRequest("POST", h.app.URL+"/api/mood", strings.NewReader(`{"seedId":"0-0"}`))
				req.AddCookie(alice)
				req.Header.Set("X-Weazl-Request", "1")
				req.Header.Set("Content-Type", "application/json")
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != 200 {
					t.Fatal(res.Status)
				}
				scan := bufio.NewScanner(res.Body)
				seen := map[string]bool{}
				id := ""
				done := false
				for scan.Scan() {
					var e moodEvent
					if err := json.Unmarshal(scan.Bytes(), &e); err != nil {
						t.Fatal(err)
					}
					if e.Error != "" {
						t.Fatal(e.Error)
					}
					if e.Playlist != nil {
						id = e.Playlist.ID
						if previous != "" && previous != id {
							t.Fatal("did not reuse Mood")
						}
						previous = id
					}
					if e.Track != nil {
						if seen[e.Track.ID] || e.Track.ID == "invented-id" {
							t.Fatalf("invalid accepted ID %s", e.Track.ID)
						}
						seen[e.Track.ID] = true
						// Read Navidrome through the app while the model stream is still open.
						if len(seen) == 2 {
							_, raw, _ := h.request("GET", "/api/library/getPlaylist?id="+id, nil, alice)
							if !strings.Contains(string(raw), e.Track.ID) {
								t.Fatalf("track not persisted before event: %s", raw)
							}
						}
					}
					if e.Type == "done" {
						done = true
					}
				}
				res.Body.Close()
				if scan.Err() != nil {
					t.Fatal(scan.Err())
				}
				if !done || len(seen) != 20 {
					t.Fatalf("done=%v tracks=%d", done, len(seen))
				}
			}
		})
	}
}
