package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

//go:embed presets.json
var defaults []byte

type station struct {
	ID      string `json:"id,omitempty"`
	Version int64  `json:"version,omitempty"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Preset  bool   `json:"preset"`
}
type userState struct {
	StationsVersion int64             `json:"stationsVersion,omitempty"`
	Stations        []station         `json:"stations"`
	Queue           []json.RawMessage `json:"queue"`
	Current         json.RawMessage   `json:"current"`
}

// globalConfig is the installation-wide configuration introduced by the
// admin-managed connection model. It is kept in the same encrypted store as
// account records so upgrades do not add a second persistence mechanism.
type globalConfig struct {
	Version       int        `json:"version"`
	NavidromeURL  string     `json:"navidromeURL,omitempty"`
	LLM           *llmConfig `json:"llm,omitempty"`
	AdminUsername string     `json:"adminUsername,omitempty"`
}
type store struct {
	mu   sync.Mutex
	dir  string
	aead cipher.AEAD
}

func newStore(dir string) (*store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, "key")
	key, err := os.ReadFile(keyPath)
	if os.IsNotExist(err) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		var f *os.File
		f, err = os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_, err = f.Write(key)
			if e := f.Close(); err == nil {
				err = e
			}
		}
	}
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &store{dir: dir, aead: aead}, nil
}
func (st *store) readRecord(user string, target any) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	path := filepath.Join(st.dir, digest(user)+".bin")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	n := st.aead.NonceSize()
	if len(b) < n {
		return errors.New("damaged state")
	}
	plain, err := st.aead.Open(nil, b[:n], b[n:], []byte(user))
	if err != nil {
		return err
	}
	err = json.Unmarshal(plain, target)
	return err
}
func (st *store) writeRecord(user string, state any) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	plain, err := json.Marshal(state)
	if err != nil {
		return err
	}
	nonce := make([]byte, st.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	b := st.aead.Seal(nonce, nonce, plain, []byte(user))
	f, err := os.CreateTemp(st.dir, "state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(st.dir, digest(user)+".bin"))
}
func (st *store) read(user string) (userState, error) {
	var state userState
	err := st.readRecord(user, &state)
	if os.IsNotExist(err) {
		err = json.Unmarshal(defaults, &state.Stations)
		state.Queue = []json.RawMessage{}
	}
	return state, err
}
func (st *store) write(user string, state userState) error { return st.writeRecord(user, state) }
func (st *store) readGlobal() (globalConfig, error) {
	var cfg globalConfig
	err := st.readRecord("global:config", &cfg)
	return cfg, err
}
func (st *store) writeGlobal(cfg globalConfig) error {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return st.writeRecord("global:config", cfg)
}
func (se *session) stateKey() string {
	return "state:" + se.User + ":" + se.ServerURL + "/" + se.NavUser
}
func (s *Server) getState(w http.ResponseWriter, r *http.Request, se *session) {
	state, err := s.store.read(se.stateKey())
	if err != nil {
		fail(w, 500, "Could not read saved settings")
		return
	}
	s.v1.mu.Lock()
	c, e := s.v1.collection(se)
	s.v1.mu.Unlock()
	if e != nil {
		fail(w, 503, "Could not read saved stations")
		return
	}
	if device := r.Header.Get("X-Weazl-Device"); uuidKey.MatchString(device) {
		var queue userState
		if e := s.v1.get("queue", se.stateKey()+":browser:"+device, accountID(se.User), &queue); e == nil {
			state.Queue = queue.Queue
			state.Current = queue.Current
		}
	}
	state.StationsVersion = c.Version
	state.Stations = legacyStations(c)
	jsonOut(w, 200, state)
}
func (s *Server) putState(w http.ResponseWriter, r *http.Request, se *session) {
	var state userState
	if decode(r, &state) != nil || len(state.Stations) > 200 || len(state.Queue) > 1000 {
		fail(w, 400, "Settings too large or invalid")
		return
	}
	if !validSavedTrack(state.Current, true) {
		fail(w, 400, "Invalid current track")
		return
	}
	for _, track := range state.Queue {
		if !validSavedTrack(track, false) {
			fail(w, 400, "Invalid queued track")
			return
		}
	}
	count := 0
	for _, station := range state.Stations {
		if station.Name == "" || len(station.Name) > 200 || len(station.URL) > 2048 || validRadioURL(station.URL) != nil {
			fail(w, 400, "Invalid station name or URL")
			return
		}
		if station.Preset {
			count++
		}
	}
	if count > 8 {
		fail(w, 400, "Only eight radio presets are allowed")
		return
	}
	s.v1.mu.Lock()
	defer s.v1.mu.Unlock()
	c, err := s.v1.collection(se)
	if err != nil {
		fail(w, 503, "Could not read saved stations")
		return
	}
	if (state.StationsVersion != 0 && state.StationsVersion != c.Version) || (state.StationsVersion == 0 && c.Version > 1) {
		fail(w, 409, "Presets changed on another device. Reload before saving.")
		return
	}
	next := stationCollection{Version: c.Version, Stations: []nativeStation{}}
	for i, st := range state.Stations {
		id := st.ID
		version := int64(1)
		for _, previous := range c.Stations {
			if previous.ID == id || (id == "" && previous.URL == st.URL) {
				id = previous.ID
				version = previous.Version
				if previous.Name != st.Name || previous.URL != st.URL || previous.Preset != st.Preset {
					version++
				}
				break
			}
		}
		if id == "" {
			id = randomID()
		}
		next.Stations = append(next.Stations, nativeStation{id, st.Name, st.URL, st.Preset, i, version})
	}
	before, _ := json.Marshal(c.Stations)
	after, _ := json.Marshal(next.Stations)
	if string(before) != string(after) {
		next.Version++
	}
	state.StationsVersion = next.Version
	state.Stations = legacyStations(next)
	archival := state
	if device := r.Header.Get("X-Weazl-Device"); uuidKey.MatchString(device) {
		queue := state
		queue.Stations = nil
		if e := s.v1.put("queue", se.stateKey()+":browser:"+device, accountID(se.User), queue); e != nil {
			fail(w, 503, "Could not save device queue")
			return
		}
		previous, e := s.store.read(se.stateKey())
		if e != nil {
			fail(w, 503, "Could not preserve saved settings")
			return
		}
		archival.Queue = previous.Queue
		archival.Current = previous.Current
	}
	if err := s.store.write(se.stateKey(), archival); err != nil {
		fail(w, 500, "Could not save settings")
		return
	}
	if err := s.v1.put("stations", se.stateKey(), accountID(se.User), next); err != nil {
		fail(w, 503, "Could not save stations")
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "stationsVersion": next.Version, "stations": state.Stations})
}

func legacyStations(c stationCollection) []station {
	out := []station{}
	for _, s := range c.Stations {
		out = append(out, station{ID: s.ID, Version: s.Version, Name: s.Name, URL: s.URL, Preset: s.Preset})
	}
	return out
}
