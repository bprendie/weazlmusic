package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func compactFixture(t *testing.T, count int) (*apiV1, recordingJob, []byte) {
	t.Helper()
	h := setup(t)
	a := h.app.Config.Handler.(*Server).v1
	a.cancel()
	a.wg.Wait() // Drive finalization explicitly; no scheduler races in this transaction fixture.
	dir := t.TempDir()
	sample := filepath.Join(dir, "sample.m4a")
	source := filepath.Join("..", "..", "fixtures", "v1", "media", "aac-lc-160k-stereo.m4a")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-stream_loop", "-1", "-i", source, "-t", "30", "-c:a", "copy", sample).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	data, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	now := time.Now().UTC()
	j := a.makeJob("compact-job", "", scheduleInput{Name: "Ten minute fixture", Starts: now, Ends: now.Add(time.Duration(count)*30*time.Second + 5*time.Second)}, &session{User: "alice"}, []nativeStation{{ID: "station-a", Name: "Tone", Preset: true}})
	j.State = "finalizing"
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := 0; i < count; i++ {
		id := digest("compact-source-" + itoa(int64(i)))[:40]
		start := int64(i)*30000 + int64(i)/5 // Include the capture's millisecond rounding seams.
		s := recordingSegment{id, id, start, 30000, 0, int64(len(data)), hash, "audio/mp4", "aac-lc"}
		j.Stations[0].Segments = append(j.Stations[0].Segments, s)
		j.Stations[0].Bytes += s.Bytes
		j.Stations[0].Duration += s.Duration
		j.Bytes += s.Bytes
		if err = os.WriteFile(filepath.Join(a.s.cfg.DataDir, "recordings", id+".m4a"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if err = a.put("asset", id, accountID(j.Username), assetRecord{id, j.ID, id + ".m4a", s.Bytes, hash, 30000}); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.put("job", j.ID, accountID(j.Username), j); err != nil {
		t.Fatal(err)
	}
	return a, j, data
}

func packetHashes(t *testing.T, path string) []string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-show_packets", "-show_data_hash", "sha256", "-show_entries", "packet=data_hash", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(out))
}

func TestRecorderTenMinuteCompactionAtomicRetryAndPackets(t *testing.T) {
	a, j, data := compactFixture(t, 21)
	original := append([]recordingSegment(nil), j.Stations[0].Segments...)
	replacements := a.compactRecording(context.Background(), &j)
	if len(replacements) != 1 || len(j.Stations[0].Segments) != 2 {
		t.Fatalf("expected ten-minute chunk and tail: %d %+v", len(replacements), j.Stations[0])
	}
	merged := j.Stations[0].Segments[0]
	if merged.Start != original[0].Start || merged.Duration != 600003 {
		t.Fatal("timeline shifted", merged)
	}
	path := filepath.Join(a.s.cfg.DataDir, "recordings", merged.AssetID+".m4a")
	before := packetHashes(t, filepath.Join(a.s.cfg.DataDir, "recordings", original[0].AssetID+".m4a"))
	after := packetHashes(t, path)
	if len(after) != 20*len(before) {
		t.Fatalf("packet count %d != %d", len(after), 20*len(before))
	}
	for i, hash := range after {
		if hash != before[i%len(before)] {
			t.Fatalf("AAC packet %d was changed", i)
		}
	}
	for _, offset := range []string{"29.8", "30.2", "299.5", "599.0"} {
		if err := exec.Command("ffmpeg", "-v", "error", "-ss", offset, "-i", path, "-t", "0.1", "-f", "null", "-").Run(); err != nil {
			t.Fatal("merged seek/decode", offset, err)
		}
	}
	// Force failure at manifest publication; original records/files must survive.
	a.mu.Lock()
	if _, err := a.db.Exec("CREATE TRIGGER reject_compact BEFORE INSERT ON records WHEN NEW.kind='manifest' BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	a.finalize(&j, replacements...)
	var persisted recordingJob
	if err := a.get("job", j.ID, "*", &persisted); err != nil || persisted.State != "finalizing" || len(persisted.Stations[0].Segments) != 21 {
		t.Fatal("failed commit changed capture", err)
	}
	var asset assetRecord
	if a.get("asset", merged.AssetID, "*", &asset) == nil {
		t.Fatal("replacement leaked from rolled-back transaction")
	}
	a.cleanupRecordingGarbage(1000)
	a.mu.Unlock()
	for _, old := range original {
		b, err := os.ReadFile(filepath.Join(a.s.cfg.DataDir, "recordings", old.AssetID+".m4a"))
		if err != nil || len(b) != len(data) {
			t.Fatal("source deleted before commit", err)
		}
	}
	// Retry from durable state, as after process death with prepared output on disk.
	replacements = a.compactRecording(context.Background(), &persisted)
	if persisted.Stations[0].Segments[0].AssetID != merged.AssetID {
		t.Fatal("retry changed deterministic identity")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.db.Exec("DROP TRIGGER reject_compact"); err != nil {
		t.Fatal(err)
	}
	a.finalize(&persisted, replacements...)
	var manifest recordingManifest
	if err := a.get("manifest", j.ID, "*", &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Tracks[0].Segments) != 2 || manifest.Tracks[0].Segments[1].Start != original[20].Start {
		t.Fatal("final manifest", manifest)
	}
	a.cleanupRecordingGarbage(7)
	a.cleanupRecordingGarbage(1000)
	for _, old := range original[:20] {
		if _, err := os.Stat(filepath.Join(a.s.cfg.DataDir, "recordings", old.AssetID+".m4a")); !os.IsNotExist(err) {
			t.Fatal("old file not reclaimed", err)
		}
	}
	if a.get("asset", merged.AssetID, "*", &asset) != nil || asset.Hash != merged.Hash {
		t.Fatal("replacement asset missing")
	}
	// Optional ignored browser artifact: exercise actual ten-minute output in Chromium.
	if artifact := os.Getenv("RECORDER_COMPACT_ARTIFACT_DIR"); artifact != "" {
		if err := os.MkdirAll(artifact, 0700); err != nil {
			t.Fatal(err)
		}
		for _, s := range manifest.Tracks[0].Segments {
			b, err := os.ReadFile(filepath.Join(a.s.cfg.DataDir, "recordings", s.AssetID+".m4a"))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(artifact, s.AssetID+".m4a"), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
		b, _ := json.Marshal(manifest)
		if err := os.WriteFile(filepath.Join(artifact, "manifest.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecorderCompactionPreservesRealGapsAndFallsBack(t *testing.T) {
	a, j, _ := compactFixture(t, 4)
	j.Stations[0].Segments[2].Start += 5000
	j.Stations[0].Segments[3].Start += 5000
	replacements := a.compactRecording(context.Background(), &j)
	if len(replacements) != 2 || len(j.Stations[0].Segments) != 2 {
		t.Fatal("real gap prevented neither appropriate grouping nor merge", j.Stations[0].Error)
	}
	a.mu.Lock()
	a.finalize(&j, replacements...)
	var m recordingManifest
	err := a.get("manifest", j.ID, "*", &m)
	a.mu.Unlock()
	if err != nil || len(m.Tracks[0].Gaps) == 0 || m.Tracks[0].Gaps[0].Start != 60000 || m.Tracks[0].Gaps[0].Duration != 5000 {
		t.Fatal("capture gap compressed", err, m)
	}
	a.mu.Lock()
	a.cleanupRecordingGarbage(1000)
	err = a.deleteRecording(&j)
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range j.Stations[0].Segments {
		if _, err = os.Stat(filepath.Join(a.s.cfg.DataDir, "recordings", s.AssetID+".m4a")); !os.IsNotExist(err) {
			t.Fatal("delete left merged asset", err)
		}
	}
}

func TestRecorderCompactionLowSpaceKeepsOriginals(t *testing.T) {
	a, j, _ := compactFixture(t, 2)
	original := j.Stations[0].Segments[0]
	a.s.cfg.ReserveBytes = 1 << 62
	replacements := a.compactRecording(context.Background(), &j)
	if len(replacements) != 0 || len(j.Stations[0].Segments) != 2 || j.Stations[0].Segments[0] != original || j.Stations[0].Error == "" {
		t.Fatal("original audio not preserved under reserve pressure")
	}
}
