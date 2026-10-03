package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckedInV1ContractFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "fixtures", "v1")
	files, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil || len(files) < 30 {
		t.Fatal("contract fixtures missing", err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil || !json.Valid(raw) {
			t.Fatal("invalid fixture", file, err)
		}
	}
	var capability struct {
		Data struct {
			Contract string `json:"contractRevision"`
		} `json:"data"`
	}
	raw, _ := os.ReadFile(filepath.Join(root, "capabilities.json"))
	if err := json.Unmarshal(raw, &capability); err != nil || capability.Data.Contract != contractRevision {
		t.Fatal("fixture revision differs from routes", err)
	}
	for _, name := range []string{"manifest-complete", "manifest-partial"} {
		raw, err := os.ReadFile(filepath.Join(root, name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m := decodeData[recordingManifest](t, raw)
		var total int64
		for _, track := range m.Tracks {
			coverage := make([]recordingGap, 0, len(track.Gaps)+len(track.Segments))
			coverage = append(coverage, track.Gaps...)
			for _, s := range track.Segments {
				if s.Duration <= 0 || s.Bytes <= 0 || len(s.Hash) != 64 || s.ContentType != "audio/mp4" || s.Codec != "aac-lc" {
					t.Fatal("invalid asset", s)
				}
				coverage = append(coverage, recordingGap{Start: s.Start, Duration: s.Duration})
				total += s.Bytes
			}
			// Walk the partition by start; every station must cover the whole logical duration.
			cursor := int64(0)
			for len(coverage) > 0 {
				found := -1
				for i, c := range coverage {
					if c.Start == cursor && c.Duration > 0 {
						found = i
						break
					}
				}
				if found < 0 {
					t.Fatal("fixture timeline is compressed or overlapping", name, cursor)
				}
				cursor += coverage[found].Duration
				coverage = append(coverage[:found], coverage[found+1:]...)
			}
			if cursor != m.Duration {
				t.Fatal("fixture duration differs", name, cursor, m.Duration)
			}
		}
		if total != m.Bytes {
			t.Fatal("total size differs")
		}
		if name == "manifest-complete" {
			media, err := os.ReadFile(filepath.Join(root, "media", "aac-lc-160k-stereo.m4a"))
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(media)
			s := m.Tracks[0].Segments[0]
			if int64(len(media)) != s.Bytes || hex.EncodeToString(hash[:]) != s.Hash {
				t.Fatal("checked-in sample changed")
			}
		}
	}
}
