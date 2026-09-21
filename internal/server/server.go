package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Config struct {
	DataDir      string
	SecureCookie bool
}
type Server struct {
	moodJobs   map[string]string
	cfg        Config
	upstream   *http.Client
	llm        *http.Client
	radio      *http.Client
	mu         sync.Mutex
	sessions   map[string]*session
	attempts   map[string]attempt
	accountsMu sync.Mutex
	store      *store
}

func New(cfg Config, assets fs.FS) (http.Handler, error) {
	st, err := newStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	bootstrap := &Server{store: st}
	if err = bootstrap.ensureDefaultAdmin(); err != nil {
		return nil, err
	}
	transport := navidromeTransport()
	llmTransport := navidromeTransport()
	llmTransport.ResponseHeaderTimeout = 120 * time.Second
	s := &Server{llm: &http.Client{Transport: llmTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, moodJobs: map[string]string{}, cfg: cfg, store: st, sessions: map[string]*session{}, attempts: map[string]attempt{}, upstream: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, radio: radioClient()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/register", s.register)
	mux.HandleFunc("PUT /api/password", s.auth(s.changePassword))
	mux.HandleFunc("GET /api/admin/config", s.admin(s.adminConfig))
	mux.HandleFunc("PUT /api/admin/config", s.admin(s.saveAdminConfig))
	mux.HandleFunc("PUT /api/connection", s.auth(s.configureConnection))
	mux.HandleFunc("GET /api/llm", s.auth(s.llmSettings))
	mux.HandleFunc("PUT /api/llm", s.auth(s.saveLLM))
	mux.HandleFunc("POST /api/llm/models", s.auth(s.discoverLLM))
	mux.HandleFunc("POST /api/mood", s.connected(s.buildMood))
	mux.HandleFunc("POST /api/logout", s.auth(s.logout))
	mux.HandleFunc("GET /api/me", s.auth(func(w http.ResponseWriter, r *http.Request, se *session) { jsonOut(w, 200, identity(se)) }))
	mux.HandleFunc("GET /api/library/{method}", s.connected(s.library))
	mux.HandleFunc("POST /api/favorite", s.connected(s.favorite))
	mux.HandleFunc("POST /api/playlists", s.connected(s.playlistWrite))
	mux.HandleFunc("POST /api/playlist-delete", s.connected(s.playlistDelete))
	mux.HandleFunc("GET /api/media/{kind}", s.connected(s.media))
	mux.HandleFunc("GET /api/state", s.auth(s.getState))
	mux.HandleFunc("PUT /api/state", s.auth(s.putState))
	mux.HandleFunc("GET /api/radio/directory", s.auth(s.directory))
	mux.HandleFunc("GET /api/radio/stream", s.auth(s.radioStream))
	mux.HandleFunc("GET /api/radio/events", s.auth(s.radioEvents))
	mux.Handle("GET /", http.FileServer(http.FS(assets)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; media-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("X-Weazl-Request") != "1" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "Request rejected")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				if e != nil || u.Host != r.Host {
					fail(w, 403, "Request origin rejected")
					return
				}
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		mux.ServeHTTP(w, r)
	}), nil
}
func jsonOut(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, msg string) {
	jsonOut(w, code, map[string]string{"error": msg})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
