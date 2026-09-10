// Package testnav provides an isolated Navidrome fixture for integration tests.
package testnav

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Playlist struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Owner     string           `json:"owner"`
	SongCount int              `json:"songCount"`
	Entry     []map[string]any `json:"entry"`
}
type Server struct {
	mu        sync.Mutex
	Playlists map[string]*Playlist
	Stars     map[string]map[string]bool
	Next      int
	Requests  []string
}

func New() *Server {
	return &Server{Playlists: map[string]*Playlist{}, Stars: map[string]map[string]bool{}, Next: 1}
}
func songs() []map[string]any {
	out := []map[string]any{}
	names := []string{"Tycho", "Boards of Canada", "Radiohead", "Massive Attack"}
	titles := []string{"Dive", "Music Has the Right to Children", "In Rainbows", "Mezzanine"}
	for a := 0; a < 12; a++ {
		for i := 0; i < 3; i++ {
			out = append(out, map[string]any{"id": fmt.Sprintf("%d-%d", a, i), "title": fmt.Sprintf("Track %d", a*3+i+1), "artist": names[a%4], "album": titles[a%4], "albumId": strconv.Itoa(a), "coverArt": strconv.Itoa(a), "duration": 6})
		}
	}
	return out
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if serveLLM(w, r) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q := r.URL.Query()
	user := q.Get("u")
	method := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/rest/"), ".view")
	out := map[string]any{"status": "ok", "version": "1.16.1"}
	send := func() {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": out})
	}
	h := md5.Sum([]byte("test-password" + q.Get("s")))
	if (user != "alice" && user != "bob") || q.Get("t") != hex.EncodeToString(h[:]) {
		out["status"] = "failed"
		out["error"] = map[string]any{"code": 40}
		send()
		return
	}
	s.Requests = append(s.Requests, user+":"+method)
	all := songs()
	switch method {
	case "ping":
	case "getUser":
		out["user"] = map[string]string{"username": user}
	case "getAlbumList2":
		albums := []map[string]any{}
		offset, _ := strconv.Atoi(q.Get("offset"))
		size, _ := strconv.Atoi(q.Get("size"))
		if size == 0 {
			size = 40
		}
		for i := offset; i < 12 && i < offset+size; i++ {
			albums = append(albums, map[string]any{"id": strconv.Itoa(i), "name": albumName(all[i*3]["album"].(string), i), "artist": all[i*3]["artist"], "year": 2000 + i, "coverArt": strconv.Itoa(i)})
		}
		out["albumList2"] = map[string]any{"album": albums}
	case "getSong":
		for _, t := range all {
			if t["id"] == q.Get("id") {
				out["song"] = t
			}
		}
	case "getRandomSongs", "getSimilarSongs2":
		rand.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
		size, _ := strconv.Atoi(q.Get("size"))
		if size > 0 && size < len(all) {
			all = all[:size]
		}
		key := "randomSongs"
		if method == "getSimilarSongs2" {
			key = "similarSongs2"
		}
		out[key] = map[string]any{"song": all}
	case "getAlbum":
		entries := []map[string]any{}
		for _, t := range all {
			if t["albumId"] == q.Get("id") {
				entries = append(entries, t)
			}
		}
		if len(entries) > 0 {
			out["album"] = map[string]any{"id": q.Get("id"), "name": entries[0]["album"], "artist": entries[0]["artist"], "song": entries}
		}
	case "search3":
		entries := []map[string]any{}
		for _, t := range all {
			if strings.Contains(strings.ToLower(fmt.Sprint(t)), strings.ToLower(q.Get("query"))) {
				entries = append(entries, t)
			}
		}
		out["searchResult3"] = map[string]any{"song": entries}
	case "getStarred2":
		entries := []map[string]any{}
		for _, t := range all {
			if s.Stars[user][t["id"].(string)] {
				entries = append(entries, t)
			}
		}
		out["starred2"] = map[string]any{"song": entries}
	case "star", "unstar":
		if s.Stars[user] == nil {
			s.Stars[user] = map[string]bool{}
		}
		s.Stars[user][q.Get("id")] = method == "star"
	case "getPlaylists":
		list := []*Playlist{}
		for _, p := range s.Playlists {
			if p.Owner == user {
				list = append(list, p)
			}
		}
		out["playlists"] = map[string]any{"playlist": list}
	case "getPlaylist":
		if p := s.Playlists[q.Get("id")]; p != nil {
			out["playlist"] = p
		} else {
			out["status"] = "failed"
			out["error"] = map[string]int{"code": 70}
		}
	case "createPlaylist":
		id := strconv.Itoa(s.Next)
		s.Next++
		if existing := q.Get("playlistId"); existing != "" {
			id = existing
		}
		p := &Playlist{ID: id, Name: q.Get("name"), Owner: user, Entry: []map[string]any{}}
		for _, id := range q["songId"] {
			for _, t := range all {
				if t["id"] == id {
					p.Entry = append(p.Entry, t)
				}
			}
		}
		p.SongCount = len(p.Entry)
		s.Playlists[p.ID] = p
		out["playlist"] = p
	case "updatePlaylist":
		p := s.Playlists[q.Get("playlistId")]
		if p == nil || p.Owner != user {
			out["status"] = "failed"
			out["error"] = map[string]int{"code": 50}
			break
		}
		if name := q.Get("name"); name != "" {
			p.Name = name
		}
		remove := map[int]bool{}
		for _, v := range q["songIndexToRemove"] {
			n, _ := strconv.Atoi(v)
			remove[n] = true
		}
		entries := []map[string]any{}
		for i, t := range p.Entry {
			if !remove[i] {
				entries = append(entries, t)
			}
		}
		for _, id := range q["songIdToAdd"] {
			for _, t := range all {
				if t["id"] == id {
					entries = append(entries, t)
				}
			}
		}
		p.Entry = entries
		p.SongCount = len(entries)
	case "deletePlaylist":
		p := s.Playlists[q.Get("id")]
		if p != nil && p.Owner == user {
			delete(s.Playlists, p.ID)
		} else {
			out["status"] = "failed"
			out["error"] = map[string]int{"code": 50}
		}
	case "stream":
		w.Header().Set("Content-Type", "audio/wav")
		http.ServeContent(w, r, "track.wav", time.Time{}, bytes.NewReader(wav()))
		return
	case "getCoverArt":
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="400"><rect width="400" height="400" fill="#525a71"/><circle cx="200" cy="180" r="95" fill="#e6e68a"/><path d="M0 290h400v110H0" fill="#2d263c"/></svg>`)
		return
	default:
		out["status"] = "failed"
		out["error"] = map[string]int{"code": 70}
	}
	send()
}
func wav() []byte {
	const size = 60 * 8000 * 2
	b := new(bytes.Buffer)
	b.WriteString("RIFF")
	_ = binary.Write(b, binary.LittleEndian, uint32(size+36))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		_ = binary.Write(b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(b, binary.LittleEndian, uint32(size))
	b.Write(make([]byte, size))
	return b.Bytes()
}

func albumName(name string, index int) string {
	if index < 4 {
		return name
	}
	return fmt.Sprintf("%s / Archive %d", name, index)
}
