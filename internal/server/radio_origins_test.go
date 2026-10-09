package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRadioPrivateOriginPolicy(t *testing.T) {
	origins, err := parseRadioOrigins(" https://Radio.Example/,http://radio.example:8000 ")
	if err != nil || strings.Join(origins, ",") != "https://radio.example:443,http://radio.example:8000" {
		t.Fatal(origins, err)
	}
	for _, raw := range []string{"https://radio.example/live", "https://user:pass@radio.example", "https://*.example", "http://radio.example?", "http://radio.example#x", "file:///radio", "https://radio.example:bad"} {
		if _, err := parseRadioOrigins(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"10.0.0.3", "172.16.0.10", "192.168.0.10", "fd00::3", "::ffff:10.0.0.3"} {
		ip := net.ParseIP(raw)
		if radioIPAllowed(ip, false) || !radioIPAllowed(ip, true) {
			t.Errorf("private policy: %s", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "::1", "169.254.169.254", "fe80::1", "100.64.0.1", "0.0.0.0", "224.0.0.1", "2001:db8::1"} {
		if radioIPAllowed(net.ParseIP(raw), true) {
			t.Errorf("allowed restricted address %s", raw)
		}
	}
}

func TestRadioOriginRedirectAndPlaylistIsolation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "http://other.example/audio", http.StatusFound)
		case "/list.m3u":
			w.Header().Set("Content-Type", "audio/x-mpegurl")
			io.WriteString(w, "http://other.example/audio\n")
		default:
			w.Header().Set("Content-Type", "audio/mpeg")
			io.WriteString(w, "audio")
		}
	}))
	defer upstream.Close()
	origins, _ := parseRadioOrigins("http://radio.example")
	client := radioClient(origins...)
	defer client.CloseIdleConnections()
	transport := client.Transport.(*radioOriginTransport)
	var publicCalls, privateCalls int
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(upstream.URL, "http://"))
	}
	transport.public.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		publicCalls++
		return dial(ctx, network, address)
	}
	transport.private.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		privateCalls++
		return dial(ctx, network, address)
	}
	s := &Server{radio: client}
	for _, path := range []string{"/audio", "/redirect", "/list.m3u"} {
		res, err := s.resolveRadio(context.Background(), "http://radio.example"+path)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || string(b) != "audio" {
			t.Fatal(string(b), err)
		}
	}
	if privateCalls != 1 || publicCalls != 1 {
		t.Fatalf("private=%d public=%d", privateCalls, publicCalls)
	}
	for _, raw := range []string{"https://radio.example/audio", "http://radio.example:8000/audio", "http://other.example/audio"} {
		req, _ := http.NewRequest("GET", raw, nil)
		if transport.allowed[radioOrigin(req.URL)] {
			t.Fatalf("exception escaped origin: %s", raw)
		}
	}
	// Even an explicitly configured origin cannot reach loopback/metadata services.
	for _, raw := range []string{"http://127.0.0.1", "http://169.254.169.254"} {
		origins, _ := parseRadioOrigins(raw)
		guarded := radioClient(origins...)
		guarded.Timeout = time.Second
		if res, err := guarded.Get(raw); err == nil {
			res.Body.Close()
			t.Fatalf("restricted address reached: %s", raw)
		}
		guarded.CloseIdleConnections()
	}
}

// Explicit opt-in bounded live check, never part of ordinary automated tests.
func TestRadioPrivateOriginLive(t *testing.T) {
	raw := os.Getenv("WEAZL_TEST_RADIO_URL")
	if raw == "" {
		t.Skip("no live radio URL configured")
	}
	origins, err := parseRadioOrigins(os.Getenv("WEAZL_TEST_RADIO_ORIGIN"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := &Server{radio: radioClient(origins...)}
	defer s.radio.CloseIdleConnections()
	res, err := s.resolveRadio(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "audio/") {
		t.Fatal("not audio")
	}
	b := make([]byte, 32768)
	if _, err := io.ReadFull(res.Body, b); err != nil {
		t.Fatal(err)
	}
	t.Logf("received %d stream bytes (%s)", len(b), res.Header.Get("Content-Type"))
}
