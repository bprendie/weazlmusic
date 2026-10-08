package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRecorderAACAndVBRInputs128kFDKProfile(t *testing.T) {
	if !mediaToolsAvailable() {
		t.Fatal("ffmpeg, ffprobe and fdkaac required")
	}
	for _, format := range []string{"aac", "vbr-mp3"} {
		t.Run(format, func(t *testing.T) {
			args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000", "-t", "6"}
			if format == "aac" {
				args = append(args, "-c:a", "aac", "-b:a", "96k", "-f", "adts", "pipe:1")
			} else {
				args = append(args, "-c:a", "libmp3lame", "-q:a", "4", "-f", "mp3", "pipe:1")
			}
			input, err := exec.Command("ffmpeg", args...).Output()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			encoder, err := startRecorderEncoder(dir, 2)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = encoder.Input.Write(input); err != nil {
				t.Fatal(err)
			}
			encoder.Input.Close()
			if err = encoder.wait(); err != nil {
				t.Fatal(err)
			}
			files, err := filepath.Glob(filepath.Join(dir, "*.m4a"))
			if err != nil || len(files) < 2 {
				t.Fatal("segments missing", files, err)
			}
			for _, file := range files {
				bitrate, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=bit_rate", "-of", "csv=p=0", file).Output()
				if err != nil {
					t.Fatal(err)
				}
				rate, err := strconv.Atoi(strings.TrimSpace(string(bitrate)))
				if err != nil || rate < 115000 || rate > 141000 {
					t.Fatalf("expected approximately 128 kbps AAC, got %s (%v)", bitrate, err)
				}
				if _, err := probeMedia(file); err != nil {
					t.Fatal("not seekable AAC-LC 44100 stereo", err)
				}
				if err := exec.Command("ffmpeg", "-v", "error", "-ss", "0.5", "-i", file, "-t", "0.1", "-f", "null", "-").Run(); err != nil {
					t.Fatal("decode/seek failed", err)
				}
			}
			broken := filepath.Join(dir, "unfinished.m4a")
			if err := os.WriteFile(broken, []byte("unfinished fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := probeMedia(broken); err == nil {
				t.Fatal("partial file accepted")
			}
		})
	}
}
