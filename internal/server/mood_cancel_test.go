package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMoodCancelPreservesAcceptedTracks(t *testing.T) {
	h := setup(t)
	c := h.login("alice")
	code, b, _ := h.request("PUT", "/api/llm", map[string]string{"provider": "ollama", "url": h.upstream, "model": "fixture-model"}, c)
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	req, _ := http.NewRequest("POST", h.app.URL+"/api/mood", strings.NewReader(`{"seedId":"0-0"}`))
	req.AddCookie(c)
	req.Header.Set("X-Weazl-Request", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	scan := bufio.NewScanner(res.Body)
	id := ""
	for scan.Scan() {
		var e moodEvent
		_ = json.Unmarshal(scan.Bytes(), &e)
		if e.Error != "" {
			t.Fatal(e.Error)
		}
		if e.Playlist != nil {
			id = e.Playlist.ID
		}
		if e.Type == "track" {
			break
		}
	}
	if id == "" {
		t.Fatal("no playlist")
	}
	code, _, _ = h.request("POST", "/api/mood", map[string]string{"seedId": "0-0"}, c)
	if code != 409 {
		t.Fatalf("concurrent build: %d", code)
	}
	res.Body.Close()
	// Allow the disconnected request to release its upstream stream and job lock.
	time.Sleep(200 * time.Millisecond)
	_, before, _ := h.request("GET", "/api/library/getPlaylist?id="+id, nil, c)
	time.Sleep(200 * time.Millisecond)
	_, after, _ := h.request("GET", "/api/library/getPlaylist?id="+id, nil, c)
	if string(before) != string(after) {
		t.Fatal("playlist continued changing after cancellation")
	}
	var out struct {
		Playlist struct {
			Entry []moodTrack `json:"entry"`
		} `json:"playlist"`
	}
	if json.Unmarshal(after, &out) != nil || len(out.Playlist.Entry) < 2 || len(out.Playlist.Entry) >= 20 {
		t.Fatalf("partial playlist: %s", after)
	}
}
