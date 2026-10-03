// weazlfixture runs a complete isolated WeazlTunes installation with fake
// Navidrome, six synthetic radio stations, ICY changes, stalls and reconnects.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"weazltunes.local/web/internal/server"
	"weazltunes.local/web/internal/testnav"
	"weazltunes.local/web/internal/testradio"
	"weazltunes.local/web/web"
)

func main() {
	radio, e := testradio.New()
	if e != nil {
		log.Fatal(e)
	}
	rs := httptest.NewServer(radio)
	defer rs.Close()
	up := httptest.NewUnstartedServer(testnav.New())
	up.Listener.Close()
	up.Listener, e = net.Listen("tcp", server.Env("FIXTURE_NAV_LISTEN", "127.0.0.1:4536"))
	if e != nil {
		log.Fatal(e)
	}
	up.Start()
	defer up.Close()
	target, _ := url.Parse(rs.URL)
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		dir, e = os.MkdirTemp("", "weazlfixture-*")
		if e != nil {
			log.Fatal(e)
		}
		defer os.RemoveAll(dir)
	}
	segmentSeconds, e := strconv.Atoi(server.Env("FIXTURE_SEGMENT_SECONDS", "5"))
	if e != nil || segmentSeconds < 1 || segmentSeconds > 60 {
		log.Fatal("FIXTURE_SEGMENT_SECONDS must be 1..60")
	}
	handler, e := server.New(server.Config{DataDir: dir, SegmentSeconds: segmentSeconds, RadioClient: &http.Client{Transport: testradio.Transport{Target: target, Base: http.DefaultTransport}}}, web.Files)
	if e != nil {
		log.Fatal(e)
	}
	defer handler.(interface{ Close() }).Close()
	listener, e := net.Listen("tcp", server.Env("LISTEN_ADDR", "127.0.0.1:4003"))
	if e != nil {
		log.Fatal(e)
	}
	app := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go app.Serve(listener)
	address := "http://127.0.0.1:" + fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
	admin := call(address, "POST", "/api/login", map[string]string{"username": "weazladmin", "password": "admin"}, nil, "")
	call(address, "PUT", "/api/admin/config", map[string]string{"navidromeURL": up.URL, "provider": "off"}, admin, "")
	for _, user := range []string{"alice", "bob"} {
		var login struct {
			Data struct {
				Access string `json:"accessToken"`
			}
		}
		body := request(address, "POST", "/api/v1/auth/login", map[string]string{"username": user, "password": "test-password", "deviceName": "fixture setup", "clientId": "fixture-setup"}, nil, "")
		if json.Unmarshal(body, &login) != nil || login.Data.Access == "" {
			log.Fatal("fixture login failed")
		}
		out := request(address, "GET", "/api/v1/radio/stations", nil, nil, login.Data.Access)
		var stations struct {
			Data []struct {
				ID      string `json:"id"`
				Version int64  `json:"version"`
			}
		}
		_ = json.Unmarshal(out, &stations)
		for i := 0; i < 6; i++ {
			suffix := ""
			if i == 4 {
				suffix = "?reconnect=1"
			}
			if i == 5 {
				suffix = "?stall=1"
			}
			path := "/api/v1/radio/stations/" + stations.Data[i].ID
			req := newRequest(address, "PATCH", path, map[string]any{"name": fmt.Sprintf("Fixture station %d", i+1), "streamURL": fmt.Sprintf("https://radio.fixture.invalid/station/%d%s", i, suffix), "preset": true}, nil, login.Data.Access)
			req.Header.Set("If-Match", fmt.Sprintf(`"v%d"`, stations.Data[i].Version))
			do(req)
		}
	}
	log.Printf("Fixture ready at %s; alice/bob use test-password. No production connections.", address)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = app.Shutdown(shutdown)
}
func newRequest(address, method, path string, body any, cookie *http.Cookie, token string) *http.Request {
	b, _ := json.Marshal(body)
	if body == nil {
		b = nil
	}
	r, e := http.NewRequest(method, address+path, bytes.NewReader(b))
	if e != nil {
		log.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Weazl-Request", "1")
	r.Header.Set("Idempotency-Key", fmt.Sprintf("%08x-0000-4000-8000-%012x", uint32(time.Now().UnixNano()), uint64(time.Now().UnixNano())&0xffffffffffff))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}
func do(r *http.Request) ([]byte, *http.Response) {
	res, e := http.DefaultClient.Do(r)
	if e != nil {
		log.Fatal(e)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Fatalf("fixture setup %s %s: HTTP %d", r.Method, r.URL.Path, res.StatusCode)
	}
	return b, res
}
func request(a, m, p string, b any, c *http.Cookie, t string) []byte {
	raw, _ := do(newRequest(a, m, p, b, c, t))
	return raw
}
func call(a, m, p string, b any, c *http.Cookie, t string) *http.Cookie {
	_, res := do(newRequest(a, m, p, b, c, t))
	if len(res.Cookies()) > 0 {
		return res.Cookies()[0]
	}
	return nil
}
