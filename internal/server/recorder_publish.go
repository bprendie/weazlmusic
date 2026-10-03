package server

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func diskReserveOK(dir string, reserve int64) bool {
	var fs syscall.Statfs_t
	return syscall.Statfs(dir, &fs) == nil && int64(fs.Bavail)*int64(fs.Bsize) > reserve+(1<<20)
}
func writeDurable(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func probeMedia(path string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, e := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "format=duration:stream=codec_name,profile,sample_rate,channels", "-of", "json", path).Output()
	if e != nil {
		return 0, errors.New("media validation failed")
	}
	var p struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Codec    string `json:"codec_name"`
			Profile  string `json:"profile"`
			Rate     string `json:"sample_rate"`
			Channels int    `json:"channels"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &p) != nil || len(p.Streams) != 1 || p.Streams[0].Codec != "aac" || p.Streams[0].Profile != "LC" || p.Streams[0].Rate != "44100" || p.Streams[0].Channels != 2 {
		return 0, errors.New("output is not AAC audio")
	}
	seconds, e := strconv.ParseFloat(p.Format.Duration, 64)
	if e != nil || seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, errors.New("invalid media duration")
	}
	return int64(math.Round(seconds * 1000)), nil
}
func (a *apiV1) recoverAttempt(initial recordingJob, index int, dir string) {
	anchor, e := os.ReadFile(filepath.Join(dir, "anchor.json"))
	if e != nil {
		return
	}
	var base int64
	if json.Unmarshal(anchor, &base) != nil {
		return
	}
	f, e := os.Open(filepath.Join(dir, "segments.csv"))
	if e != nil {
		return
	}
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	rows, _ := reader.ReadAll()
	f.Close()
	for _, row := range rows {
		if len(row) != 3 {
			continue
		}
		start, e := strconv.ParseFloat(row[1], 64)
		if e != nil || !isFinite(start) {
			continue
		}
		name := filepath.Base(row[0])
		if !strings.HasSuffix(name, ".m4a") {
			continue
		}
		id := digest(initial.ID + initial.Stations[index].StationID + filepath.Base(dir) + name)[:40]
		source := filepath.Join(dir, name)
		target := filepath.Join(a.s.cfg.DataDir, "recordings", id+".m4a")
		a.mu.Lock()
		var existing assetRecord
		e = a.get("asset", id, accountID(initial.Username), &existing)
		a.mu.Unlock()
		if e == nil {
			continue
		}
		// A crash after rename but before the SQLite commit leaves a recoverable immutable file.
		if _, e = os.Stat(source); os.IsNotExist(e) {
			source = target
		}
		duration, e := probeMedia(source)
		if e != nil {
			continue
		}
		file, e := os.Open(source)
		if e != nil {
			continue
		}
		h := sha256.New()
		size, e := io.Copy(h, file)
		file.Close()
		if e != nil || size <= 0 {
			continue
		}
		hash := hex.EncodeToString(h.Sum(nil))
		at := base + int64(math.Round(start*1000))
		if at < 0 || at >= initial.Duration {
			continue
		}
		duration = min(duration, initial.Duration-at)
		a.mu.Lock()
		var j recordingJob
		if a.get("job", initial.ID, accountID(initial.Username), &j) != nil || j.Tombstone {
			a.mu.Unlock()
			return
		}
		p := &j.Stations[index]
		overlap := false
		for _, seg := range p.Segments {
			if at < seg.Start+seg.Duration && at+duration > seg.Start {
				overlap = true
			}
		}
		if overlap {
			a.mu.Unlock()
			continue
		}
		status, e := a.storage()
		if e != nil || j.Bytes+size > j.Reserved || p.Bytes+size > j.Reserved/int64(j.StationCount) || status.Used+size > status.Budget || !diskReserveOK(a.s.cfg.DataDir, status.Reserve) {
			p.Error = "Recording storage limit reached."
			_ = a.put("job", j.ID, accountID(j.Username), j)
			a.mu.Unlock()
			return
		}
		file, e = os.OpenFile(source, os.O_RDWR, 0600)
		if e == nil {
			e = file.Sync()
			file.Close()
		}
		if e == nil && source != target {
			e = os.Rename(source, target)
		}
		if e == nil {
			e = syncDir(filepath.Dir(target))
		}
		if e != nil {
			a.mu.Unlock()
			continue
		}
		seg := recordingSegment{id, id, at, duration, 0, size, hash, "audio/mp4", "aac-lc"}
		p.Segments = append(p.Segments, seg)
		sort.Slice(p.Segments, func(i, k int) bool { return p.Segments[i].Start < p.Segments[k].Start })
		p.Bytes += size
		p.Duration += duration
		p.State = "recording"
		p.Error = ""
		j.Bytes += size
		j.Version++
		ar := assetRecord{id, j.ID, id + ".m4a", size, hash, duration}
		ab, ae := a.seal(ar)
		jb, je := a.seal(j)
		if ae == nil && je == nil {
			tx, e := a.db.Begin()
			if e == nil {
				_, e = tx.Exec("INSERT OR IGNORE INTO records VALUES('asset',?,?,?)", id, accountID(j.Username), ab)
				if e == nil {
					_, e = tx.Exec("UPDATE records SET body=? WHERE kind='job' AND id=?", jb, j.ID)
				}
				if e == nil {
					e = tx.Commit()
				} else {
					_ = tx.Rollback()
				}
			}
		}
		a.mu.Unlock()
	}
}
func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
