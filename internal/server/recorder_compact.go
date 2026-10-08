package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const recordingChunkMs int64 = 10 * 60 * 1000

type recordingReplacement struct {
	Asset assetRecord
	Old   []recordingSegment
}

// AAC packet boundaries and millisecond rounding can put a ten-minute group a
// few milliseconds past the target. Never join a real interruption or overlap.
func recordingGroups(segments []recordingSegment) [][]recordingSegment {
	var groups [][]recordingSegment
	for _, s := range segments {
		if len(groups) > 0 {
			group := groups[len(groups)-1]
			last := group[len(group)-1]
			gap := s.Start - last.Start - last.Duration
			if gap >= 0 && gap <= 1 && s.Start+s.Duration-group[0].Start <= recordingChunkMs+25 && s.Codec == last.Codec && s.ContentType == last.ContentType {
				groups[len(groups)-1] = append(group, s)
				continue
			}
		}
		groups = append(groups, []recordingSegment{s})
	}
	return groups
}

// Prepare new files without holding the API lock or changing published assets.
// finalize commits the manifest, replacement assets and cleanup journal together.
func (a *apiV1) compactRecording(ctx context.Context, j *recordingJob) []recordingReplacement {
	var replacements []recordingReplacement
	for i := range j.Stations {
		p := &j.Stations[i]
		var result []recordingSegment
		for _, group := range recordingGroups(p.Segments) {
			if ctx.Err() != nil {
				return replacements
			}
			if len(group) < 2 {
				result = append(result, group...)
				continue
			}
			segment, asset, err := a.compactGroup(ctx, *j, p.StationID, group)
			if err != nil {
				// A packaging failure must not destroy an otherwise usable capture.
				result = append(result, group...)
				p.Error = "Some audio remains in smaller files because final packaging could not finish."
				continue
			}
			result = append(result, segment)
			replacements = append(replacements, recordingReplacement{asset, group})
		}
		p.Segments = result
		p.Bytes = 0
		p.Duration = 0
		for _, s := range result {
			p.Bytes += s.Bytes
			p.Duration += s.Duration
		}
	}
	return replacements
}

func (a *apiV1) compactGroup(parent context.Context, j recordingJob, station string, group []recordingSegment) (segment recordingSegment, asset assetRecord, err error) {
	var identity strings.Builder
	identity.WriteString("recording-chunk-v1:" + j.ID + ":" + station)
	var inputBytes int64
	for _, s := range group {
		identity.WriteString(":" + s.AssetID)
		inputBytes += s.Bytes
	}
	id := digest(identity.String())[:40]
	root := filepath.Join(a.s.cfg.DataDir, "recordings")
	target := filepath.Join(root, id+".m4a")
	dir := filepath.Join(root, "staging", j.ID, "compact")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return
	}
	output := filepath.Join(dir, id+".m4a")
	list := filepath.Join(dir, id+".ffconcat")
	defer func() {
		os.Remove(output)
		os.Remove(list)
		if err != nil {
			os.Remove(target)
		}
	}()
	reserve := a.s.cfg.ReserveBytes
	if reserve <= 0 {
		reserve = 1 << 30
	}
	var disk syscall.Statfs_t
	if err = syscall.Statfs(root, &disk); err != nil {
		return
	}
	if int64(disk.Bavail)*int64(disk.Bsize) < reserve+inputBytes+(1<<20) {
		err = errors.New("insufficient finalization workspace")
		return
	}
	var script strings.Builder
	script.WriteString("ffconcat version 1.0\n")
	for n, s := range group {
		a.mu.Lock()
		var ar assetRecord
		err = a.get("asset", s.AssetID, accountID(j.Username), &ar)
		a.mu.Unlock()
		if err != nil {
			return
		}
		if filepath.Base(ar.Path) != ar.Path || ar.Path != s.AssetID+".m4a" || ar.Bytes != s.Bytes || ar.Session != j.ID || ar.Hash != s.Hash {
			err = errors.New("capture asset changed")
			return
		}
		// Only application-generated relative hex filenames enter the concat script.
		if _, err = hex.DecodeString(s.AssetID); err != nil {
			return
		}
		span := s.Duration
		if n+1 < len(group) {
			span = group[n+1].Start - s.Start
		}
		fmt.Fprintf(&script, "file '%s'\ninpoint %.6f\noutpoint %.6f\nduration %.6f\n", filepath.Join("..", "..", "..", ar.Path), float64(s.MediaStart)/1000, float64(s.MediaStart+s.Duration)/1000, float64(span)/1000)
	}
	if err = writeDurable(list, []byte(script.String())); err != nil {
		return
	}
	start := group[0].Start
	last := group[len(group)-1]
	span := last.Start + last.Duration - start
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-protocol_whitelist", "file", "-f", "concat", "-safe", "0", "-i", list, "-map", "0:a:0", "-c:a", "copy", "-t", fmt.Sprintf("%.6f", float64(span)/1000), "-movflags", "+faststart", output)
	if err = cmd.Run(); err != nil {
		return
	}
	var duration int64
	if duration, err = probeMedia(output); err != nil {
		return
	}
	if duration < span-1 || duration > span+25 {
		err = errors.New("merged audio does not preserve the capture timeline")
		return
	}
	var file *os.File
	if file, err = os.OpenFile(output, os.O_RDWR, 0600); err != nil {
		return
	}
	hash := sha256.New()
	var size int64
	size, err = io.Copy(hash, file)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return
	}
	if size <= 0 || size > inputBytes+(1<<16) {
		err = errors.New("unexpected merged recording size")
		return
	}
	if err = os.Rename(output, target); err != nil {
		return
	}
	if err = syncDir(root); err != nil {
		return
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	asset = assetRecord{id, j.ID, id + ".m4a", size, checksum, duration}
	segment = recordingSegment{id, id, start, span, 0, size, checksum, "audio/mp4", "aac-lc"}
	return
}
