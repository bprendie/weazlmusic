package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const podProfile = "mp3-128"
const podInputLimit int64 = 512 << 20
const podOutputLimit int64 = 256 << 20
const podReservation = podInputLimit + podOutputLimit

// Fingerprint the actual encoder build as well as every representation setting.
var podFingerprint = sync.OnceValue(detectPodEncoder)

func podEncoderVersion() string { return podFingerprint() }
func detectPodEncoder() string {
	if mediaCPUs().Cores == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := mediaCommandContext(ctx, "ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil || !strings.Contains(string(out), "libmp3lame") {
		return ""
	}
	if _, err = exec.LookPath("ffprobe"); err != nil {
		return ""
	}
	out, err = mediaCommandContext(ctx, "ffmpeg", "-version").Output()
	if err != nil {
		return ""
	}
	// The FFmpeg version string alone misses independently upgraded shared libraries.
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}
	paths := map[string]bool{binary: true}
	linked, err := exec.CommandContext(ctx, "ldd", binary).CombinedOutput()
	if err != nil && !strings.Contains(string(linked), "not a dynamic executable") && !strings.Contains(string(linked), "statically linked") {
		return ""
	}
	for _, field := range strings.Fields(string(linked)) {
		if strings.HasPrefix(field, "/") {
			paths[field] = true
		}
	}
	names := []string{}
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)
	fingerprint := string(out) + strings.Join(podEncodeArgs("SOURCE", "OUTPUT"), "\x00")
	for _, name := range names {
		_, hash, e := podHash(name)
		if e != nil {
			return ""
		}
		fingerprint += "\x00" + hash
	}
	return digest(fingerprint)
}
func podEncodeArgs(input, output string) []string {
	return []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-xerror", "-err_detect", "explode", "-protocol_whitelist", "file,pipe", "-threads", "1", "-i", input, "-map", "0:a:0", "-vn", "-map_metadata", "-1", "-map_chapters", "-1", "-c:a", "libmp3lame", "-b:a", "128k", "-ar", "44100", "-ac", "2", "-threads", "1", "-id3v2_version", "0", "-write_id3v1", "0", "-write_xing", "1", "-fs", itoa(podOutputLimit), "-f", "mp3", output}
}

type podRepresentation struct {
	ID         string `json:"representationId"`
	SourceHash string `json:"sourceSha256"`
	Encoder    string `json:"encoderRevision"`
	Bytes      int64  `json:"byteLength"`
	Hash       string `json:"sha256"`
	Duration   int64  `json:"durationMs"`
	// Logical presentation starts at decoded sample zero: Xing/LAME trim is required.
	Samples        int64     `json:"decodedSamples"`
	Delay          int64     `json:"encoderDelaySamples"`
	Padding        int64     `json:"encoderPaddingSamples"`
	DecoderSkip    int64     `json:"decoderSkipSamples"`
	DecoderDiscard int64     `json:"decoderDiscardSamples"`
	DecoderDelay   int64     `json:"referenceDecoderDelaySamples"`
	Used           time.Time `json:"lastUsedAt"`
	Pin            time.Time `json:"pinnedUntil"`
}

func podHash(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return n, hex.EncodeToString(h.Sum(nil)), err
}
func podValidate(ctx context.Context, path string) (podRepresentation, error) {
	var rep podRepresentation
	out, err := mediaCommandContext(ctx, "ffprobe", "-v", "error", "-count_packets", "-show_entries", "stream=codec_name,sample_rate,channels,bit_rate,nb_read_packets:format=duration", "-of", "json", path).Output()
	var p struct {
		Streams []struct {
			Codec    string `json:"codec_name"`
			Rate     string `json:"sample_rate"`
			Channels int    `json:"channels"`
			Bitrate  string `json:"bit_rate"`
			Packets  string `json:"nb_read_packets"`
		} `json:"streams"`
	}
	if err != nil || json.Unmarshal(out, &p) != nil || len(p.Streams) != 1 {
		return rep, errors.New("invalid MP3")
	}
	s := p.Streams[0]
	if s.Codec != "mp3" || s.Rate != "44100" || s.Channels != 2 || s.Bitrate != "128000" {
		return rep, errors.New("incorrect MP3 profile")
	}
	// Full decode is integrity evidence and measures the gapless presentation, not padded frame time.
	cmd := mediaCommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-err_detect", "explode", "-nostdin", "-protocol_whitelist", "file,pipe", "-threads", "1", "-i", path, "-map", "0:a:0", "-f", "s16le", "-acodec", "pcm_s16le", "pipe:1")
	counter := &podCounter{limit: 4 * 44100 * 4 * 60 * 60}
	cmd.Stdout = counter
	if err = cmd.Run(); err != nil || counter.n <= 0 {
		return rep, errors.New("MP3 decode failed")
	}
	rep.Samples = counter.n / 4
	// Probe just the first packet; never materialize an unbounded packet JSON array.
	first, e := mediaCommandContext(ctx, "ffprobe", "-v", "error", "-read_intervals", "%+#1", "-show_packets", "-show_entries", "packet=side_data_list", "-of", "json", path).Output()
	var trim struct {
		Packets []struct {
			Side []struct {
				Skip int64 `json:"skip_samples"`
			} `json:"side_data_list"`
		} `json:"packets"`
	}
	if e != nil || json.Unmarshal(first, &trim) != nil || len(trim.Packets) != 1 || len(trim.Packets[0].Side) != 1 {
		return rep, errors.New("missing gapless MP3 metadata")
	}
	rep.DecoderSkip = trim.Packets[0].Side[0].Skip
	packets, e := strconv.ParseInt(s.Packets, 10, 64)
	rep.DecoderDiscard = packets*1152 - rep.DecoderSkip - rep.Samples
	rep.Delay, rep.Padding, err = podLAMETrim(path)
	rep.DecoderDelay = rep.DecoderSkip - rep.Delay
	if e != nil || err != nil || rep.Delay <= 0 || rep.Delay > 4095 || rep.Padding < 0 || rep.Padding > 4095 || rep.DecoderDiscard < 0 || rep.DecoderDelay < 0 || rep.Delay+rep.Padding+rep.Samples != packets*1152 {
		return rep, errors.New("invalid MP3 trim")
	}

	rep.Duration = (rep.Samples*1000 + 22050) / 44100
	rep.Bytes, rep.Hash, err = podHash(path)
	if err != nil || rep.Bytes <= 0 || rep.Bytes >= podOutputLimit {
		return rep, errors.New("MP3 size limit")
	}
	return rep, nil
}

type podCounter struct{ n, limit int64 }

func (c *podCounter) Write(p []byte) (int, error) {
	if c.n+int64(len(p)) > c.limit {
		return 0, errors.New("duration limit")
	}
	c.n += int64(len(p))
	return len(p), nil
}

// Source decode count detects a truncated/output-limited encode and supplies an exact presentation check.
func podSourceSamples(ctx context.Context, path string) (int64, error) {
	cmd := mediaCommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-err_detect", "explode", "-nostdin", "-protocol_whitelist", "file,pipe", "-threads", "1", "-i", path, "-map", "0:a:0", "-vn", "-ar", "44100", "-ac", "2", "-f", "s16le", "pipe:1")
	c := &podCounter{limit: 4 * 44100 * 4 * 60 * 60}
	cmd.Stdout = c
	err := cmd.Run()
	if c.n == 0 && err == nil {
		err = errors.New("empty source")
	}
	return c.n / 4, err
}
func podEncodeCommand(ctx context.Context, input, output string) *exec.Cmd {
	return mediaCommandContext(ctx, "ffmpeg", podEncodeArgs(input, output)...)
}

// Read the bounded MPEG-1 stereo Info/LAME header. File tag values are encoder
// priming/padding; FFmpeg's skip side data additionally accounts for decoder delay.
func podLAMETrim(path string) (int64, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	b := make([]byte, 417)
	if _, err = io.ReadFull(f, b); err != nil {
		return 0, 0, err
	}
	at := 36
	if b[0] != 0xff || b[1]&0xfe != 0xfa || b[2]>>4 != 9 {
		return 0, 0, errors.New("invalid first MP3 frame")
	}
	if b[1]&1 == 0 {
		at += 2
	}
	if string(b[at:at+4]) != "Info" {
		return 0, 0, errors.New("missing CBR seek header")
	}
	flags := binary.BigEndian.Uint32(b[at+4 : at+8])
	at += 8
	for _, field := range []struct {
		bit  uint32
		size int
	}{{1, 4}, {2, 4}, {4, 100}, {8, 4}} {
		if flags&field.bit != 0 {
			at += field.size
		}
	}
	if at+24 > len(b) {
		return 0, 0, errors.New("invalid LAME metadata")
	}
	delay := int64(b[at+21])<<4 | int64(b[at+22]>>4)
	padding := int64(b[at+22]&15)<<8 | int64(b[at+23])
	return delay, padding, nil
}
