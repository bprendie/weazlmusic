package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func podPoll(t *testing.T, h *harness, token string, code int, b []byte) map[string]any {
	t.Helper()
	end := time.Now().Add(30 * time.Second)
	for code == 202 && time.Now().Before(end) {
		j := decodeData[map[string]any](t, b)
		time.Sleep(20 * time.Millisecond)
		code, b, _ = h.vrequest("GET", "media/preparations/"+j["preparationId"].(string), nil, token, nil)
	}
	if code != 200 {
		t.Fatalf("prepare: %d %s", code, b)
	}
	d := decodeData[map[string]any](t, b)
	if d["state"] != "ready" {
		t.Fatalf("not ready: %s", b)
	}
	return d
}
func podGet(t *testing.T, h *harness, method, path string, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(method, h.app.URL+path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, b, res.Header
}
func TestPodFiniteContract(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "pod")
	bob := h.native("bob", "other")
	hdr := map[string]string{"X-Weazl-Client": " WeAzLpOd "}
	for _, hint := range []string{"", "other", "weazlpod,other"} {
		code, b, _ := h.vrequest("POST", "media/leases", map[string]string{"kind": "trackOriginal", "resourceId": "0-0"}, c.Access, map[string]string{"X-Weazl-Client": hint})
		if code != 200 {
			t.Fatal(code, string(b))
		}
		d := decodeData[map[string]any](t, b)
		_, raw, _ := podGet(t, h, "GET", d["url"].(string), nil)
		if !bytes.HasPrefix(raw, []byte("RIFF")) {
			t.Fatal("original changed")
		}
	}
	for _, body := range []map[string]string{{"kind": "trackOriginal", "resourceId": "0-0"}, {"kind": "trackStream", "resourceId": "0-0", "quality": "original"}, {"kind": "radioLive", "resourceId": "x", "quality": "aac-128"}} {
		code, b, _ := h.vrequest("POST", "media/leases", body, c.Access, hdr)
		if code != 422 {
			t.Fatal(code, string(b))
		}
	}
	code, b, _ := h.vrequest("POST", "media/leases", map[string]string{"kind": "cover", "resourceId": "0"}, c.Access, hdr)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	body := map[string]string{"kind": "trackStream", "resourceId": "0-0"}
	start := time.Now()
	code, b, headers := h.vrequest("POST", "media/leases", body, c.Access, hdr)
	if code != 202 || headers.Get("Retry-After") != "2" {
		t.Fatal(code, string(b))
	}
	prep := decodeData[map[string]any](t, b)["preparationId"].(string)
	code2, b2, _ := h.vrequest("GET", "media/preparations/"+prep, nil, bob.Access, nil)
	if code2 != 404 {
		t.Fatal("foreign preparation", code2, string(b2))
	}
	d := podPoll(t, h, c.Access, code, b)
	t.Logf("uncached synthetic 60s WAV preparation: %s", time.Since(start))
	path := d["url"].(string)
	code, raw, full := podGet(t, h, "GET", path, nil)
	hash := sha256.Sum256(raw)
	if code != 200 || full.Get("Content-Type") != "audio/mpeg" || hex.EncodeToString(hash[:]) != d["sha256"] || float64(len(raw)) != d["byteLength"] {
		t.Fatal("wrong finite bytes", code, d)
	}
	code, head, hh := podGet(t, h, "HEAD", path, nil)
	if code != 200 || len(head) != 0 || hh.Get("Content-Length") != itoa(int64(len(raw))) {
		t.Fatal("HEAD")
	}
	for _, check := range []struct {
		rng, validator string
		status, n      int
	}{{"bytes=0-43", full.Get("ETag"), 206, 44}, {"bytes=100-199", full.Get("ETag"), 206, 100}, {"bytes=100-199", "\"changed\"", 200, len(raw)}, {"bytes=999999999-", "", 416, -1}} {
		code, part, ph := podGet(t, h, "GET", path, map[string]string{"Range": check.rng, "If-Range": check.validator})
		if code != check.status || (check.n >= 0 && len(part) != check.n) {
			t.Fatal("range", check, code, len(part))
		}
		if code == 206 && ph.Get("Content-Range") == "" {
			t.Fatal("missing range")
		}
		if code == 206 && check.n == 100 && !bytes.Equal(part, raw[100:200]) {
			t.Fatal("range bytes differ")
		}
	}
	code, b, _ = h.vrequest("POST", "media/leases", map[string]string{"kind": "trackDownload", "resourceId": "0-0", "quality": podProfile}, c.Access, nil)
	download := podPoll(t, h, c.Access, code, b)
	if download["representationId"] != d["representationId"] {
		t.Fatal("stream/download identity")
	}
	_, downloadBytes, _ := podGet(t, h, "GET", download["url"].(string), nil)
	if !bytes.Equal(raw, downloadBytes) {
		t.Fatal("stream/download bytes")
	}
	code, b, _ = h.vrequest("GET", "media/preparations/"+prep, nil, c.Access, nil)
	renewed := decodeData[map[string]any](t, b)
	if renewed["representationId"] != d["representationId"] || renewed["url"] == d["url"] {
		t.Fatal("renewal")
	}
	h.restart()
	code, again, ah := podGet(t, h, "GET", path, nil)
	if code != 200 || !bytes.Equal(raw, again) || ah.Get("ETag") != full.Get("ETag") {
		t.Fatal("restart changed lease", code)
	}
	code, _, _ = h.vrequest("DELETE", "auth/devices/"+c.Device, nil, bob.Access, nil)
	if code != 404 {
		t.Fatal("foreign revoke", code)
	}
	code, _, _ = h.vrequest("POST", "auth/logout", nil, c.Access, nil)
	if code != 204 {
		t.Fatal(code)
	}
	code, _, _ = podGet(t, h, "GET", path, nil)
	if code != 401 {
		t.Fatal("revoked media", code)
	}
}
func TestPodEncoderInputMatrix(t *testing.T) {
	if podEncoderVersion() == "" {
		t.Fatal("MP3 toolchain required")
	}
	for _, format := range []string{"wav", "flac", "m4a", "cbr.mp3", "vbr.mp3"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source."+format)
			output := filepath.Join(dir, "out.mp3")
			args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=32000", "-t", "2.345"}
			switch format {
			case "m4a":
				args = append(args, "-c:a", "aac")
			case "cbr.mp3":
				args = append(args, "-c:a", "libmp3lame", "-b:a", "192k")
			case "vbr.mp3":
				args = append(args, "-c:a", "libmp3lame", "-q:a", "3")
			}
			if err := exec.Command("ffmpeg", append(args, source)...).Run(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			samples, err := podSourceSamples(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			if out, err := podEncodeCommand(ctx, source, output).CombinedOutput(); err != nil {
				t.Fatal(err, string(out))
			}
			rep, err := podValidate(ctx, output)
			if err != nil || rep.Samples != samples {
				t.Fatal("invalid output", rep, samples, err)
			}
			for _, offset := range []string{"0.01", "1.0", "2.2"} {
				if err := exec.Command("ffmpeg", "-v", "error", "-ss", offset, "-i", output, "-t", "0.05", "-f", "null", "-").Run(); err != nil {
					t.Fatal("seek", offset, err)
				}
			}
			// Inspect every MPEG frame; a nominal average bitrate does not establish CBR.
			raw, _ := os.ReadFile(output)
			frames := 0
			for i := 0; i+4 < len(raw); {
				if raw[i] != 0xff || raw[i+1]&0xfe != 0xfa || raw[i+2]>>4 != 9 || raw[i+2]&12 != 0 {
					t.Fatalf("not MPEG1 layer III 128k/44100 at %d: %x", i, raw[i:i+4])
				}
				size := 144*128000/44100 + int((raw[i+2]>>1)&1)
				i += size
				frames++
				if i > len(raw) {
					t.Fatal("truncated frame")
				}
			}
			if frames < 2 {
				t.Fatal("missing frames")
			}
			broken := filepath.Join(dir, "broken")
			rawSource, _ := os.ReadFile(source)
			_ = os.WriteFile(broken, rawSource[:len(rawSource)/2], 0600)
			if format == "wav" || format == "m4a" {
				if _, err := podSourceSamples(ctx, broken); err == nil {
					t.Fatal("truncated source accepted")
				}
			}
		})
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "corrupt")
	_ = os.WriteFile(file, []byte("broken audio"), 0600)
	if _, err := podSourceSamples(context.Background(), file); err == nil {
		t.Fatal("corrupt accepted")
	}
}
