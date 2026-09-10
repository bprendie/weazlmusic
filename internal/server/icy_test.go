package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"
)

func icyBlock(title string) []byte {
	raw := []byte("StreamTitle='" + title + "';")
	length := (len(raw) + 15) / 16
	b := make([]byte, 1+length*16)
	b[0] = byte(length)
	copy(b[1:], raw)
	return b
}
func TestICYAudioAndMetadata(t *testing.T) {
	wire := append([]byte("AAAA"), icyBlock("Michael Jackson - Billie Jean")...)
	wire = append(wire, []byte("BBBB\x00CCCC")...)
	wire = append(wire, icyBlock("Prince - Let's Go Crazy")...)
	wire = append(wire, []byte("DD")...)
	var output bytes.Buffer
	var events []radioMetadata
	err := relayICY(&output, iotest.OneByteReader(bytes.NewReader(wire)), 4, func(m radioMetadata) { events = append(events, m) }, func() {})
	if err != io.EOF || output.String() != "AAAABBBBCCCCDD" {
		t.Fatalf("audio changed: %q, %v", output.String(), err)
	}
	if len(events) != 2 || events[0].Artist != "Michael Jackson" || events[0].Title != "Billie Jean" || events[1].Title != "Let's Go Crazy" {
		t.Fatalf("wrong metadata: %+v", events)
	}
	var truncated bytes.Buffer
	err = relayICY(&truncated, strings.NewReader("AAAA\x02short"), 4, func(radioMetadata) { t.Fatal("truncated metadata published") }, func() {})
	if err != io.ErrUnexpectedEOF || truncated.String() != "AAAA" {
		t.Fatal("truncated metadata entered audio", err)
	}
}
func TestICYTitles(t *testing.T) {
	cases := []struct {
		raw, title, artist string
		ok                 bool
	}{
		{"StreamTitle='A title without artist';", "A title without artist", "", true},
		{"StreamTitle='';", "", "", true},
		{"StreamTitle='AC/DC - Rock &amp; Roll';StreamUrl='https://example.com';", "Rock & Roll", "AC/DC", true},
		{"StreamTitle='Beyonc\xe9 - Halo';", "Halo", "Beyoncé", true},
		{"StreamUrl='https://example.com';", "", "", false},
	}
	for _, c := range cases {
		meta, ok := parseICY([]byte(c.raw))
		if ok != c.ok || meta.Title != c.title || meta.Artist != c.artist {
			t.Fatalf("%q -> %+v, %v", c.raw, meta, ok)
		}
	}
}
func TestMetadataReplayIsolationAndCleanup(t *testing.T) {
	h := newRadioHub()
	f, release, err := h.acquire("playback")
	if err != nil {
		t.Fatal(err)
	}
	expected := radioMetadata{Title: "One", Artist: "Artist"}
	f.publish(expected)
	ch, unsubscribe, err := f.subscribe()
	if err != nil {
		t.Fatal(err)
	}
	if <-ch != expected {
		t.Fatal("late subscriber missed current metadata")
	}
	f.publish(expected)
	select {
	case <-ch:
		t.Fatal("duplicate event")
	default:
	}
	second := newRadioHub()
	other, otherRelease, _ := second.acquire("playback")
	if other == f || other.hasLatest {
		t.Fatal("metadata crossed sessions")
	}
	otherRelease()
	unsubscribe()
	release()
	if len(h.feeds) != 0 || len(second.feeds) != 0 {
		t.Fatal("metadata left behind after playback")
	}
}
func TestRadioRelayUsesOneStream(t *testing.T) {
	var requests atomic.Int32
	upstreamClosed := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		defer close(upstreamClosed)
		if r.Header.Get("Icy-MetaData") != "1" {
			t.Error("metadata not requested")
		}
		w.Header().Set("Icy-Metaint", "4")
		w.Header().Set("Icy-Name", "80s 90s Super Pop Hits")
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(append(append([]byte("AAAA"), icyBlock("Michael Jackson - Billie Jean")...), []byte("BBBB\x00")...))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	se := &session{User: "local-user", Expires: time.Now().Add(time.Minute), ctx: ctx, cancel: cancel, radio: newRadioHub()}
	s := &Server{radio: upstream.Client(), sessions: map[string]*session{digest("cookie"): se}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/radio/events", s.auth(s.radioEvents))
	mux.HandleFunc("GET /api/radio/stream", s.auth(s.radioStream))
	mux.HandleFunc("POST /api/logout", s.auth(s.logout))
	app := httptest.NewServer(mux)
	defer app.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	get := func(path string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", app.URL+path, nil)
		req.AddCookie(&http.Cookie{Name: "weazl_session", Value: "cookie"})
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	id := "0123456789abcdef0123456789abcdef"
	events := get("/api/radio/events?playback=" + id)
	defer events.Body.Close()
	stream := get("/api/radio/stream?" + url.Values{"playback": {id}, "url": {upstream.URL}}.Encode())
	defer stream.Body.Close()
	audio := make([]byte, 8)
	if _, err := io.ReadFull(stream.Body, audio); err != nil || string(audio) != "AAAABBBB" {
		t.Fatal("relay did not strip metadata", err, string(audio))
	}
	scanner := bufio.NewScanner(events.Body)
	var meta radioMetadata
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &meta); err != nil {
				t.Fatal(err)
			}
			if meta.Title != "" {
				break
			}
		}
	}
	if meta.Title != "Billie Jean" || meta.Artist != "Michael Jackson" {
		t.Fatalf("missing SSE title: %+v %v", meta, scanner.Err())
	}
	if requests.Load() != 1 {
		t.Fatal("metadata opened a second upstream stream")
	}
	cancel()
	select {
	case <-upstreamClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream kept running after session ended")
	}
	_ = stream.Body.Close()
	_ = events.Body.Close()
}
