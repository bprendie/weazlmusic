package server

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) directory(w http.ResponseWriter, r *http.Request, se *session) {
	source := r.URL.Query().Get("source")
	endpoint := "https://api.somafm.com/channels.json"
	if source == "icecast" {
		endpoint = "https://dir.xiph.org/yp.xml"
	} else if source != "somafm" {
		fail(w, 400, "Unknown directory")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		fail(w, 500, "Directory request failed")
		return
	}
	res, err := s.radio.Do(req)
	if err != nil {
		fail(w, 502, "Radio directory is unavailable")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		fail(w, 502, "Radio directory is unavailable")
		return
	}
	q := strings.ToLower(r.URL.Query().Get("q"))
	out := []station{}
	if source == "somafm" {
		var data struct {
			Channels []struct {
				Title     string `json:"title"`
				Playlists []struct {
					URL    string `json:"url"`
					Format string `json:"format"`
				} `json:"playlists"`
			} `json:"channels"`
		}
		if json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&data) != nil {
			fail(w, 502, "Invalid SomaFM directory")
			return
		}
		for _, c := range data.Channels {
			if !strings.Contains(strings.ToLower(c.Title), q) {
				continue
			}
			for _, p := range c.Playlists {
				if p.Format == "mp3" && validRadioURL(p.URL) == nil {
					out = append(out, station{Name: c.Title, URL: p.URL})
					break
				}
			}
		}
	} else {
		d := xml.NewDecoder(io.LimitReader(res.Body, 32<<20))
		for len(out) < 100 {
			tok, e := d.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				fail(w, 502, "Invalid or oversized Icecast directory")
				return
			}
			start, ok := tok.(xml.StartElement)
			if !ok || start.Name.Local != "entry" {
				continue
			}
			var row struct {
				Name  string `xml:"server_name"`
				URL   string `xml:"listen_url"`
				Genre string `xml:"genre"`
			}
			if d.DecodeElement(&row, &start) != nil {
				fail(w, 502, "Invalid Icecast entry")
				return
			}
			if row.Name != "" && strings.Contains(strings.ToLower(row.Name+" "+row.Genre), q) && validRadioURL(row.URL) == nil {
				out = append(out, station{Name: row.Name, URL: row.URL})
			}
		}
	}
	jsonOut(w, 200, out)
}
