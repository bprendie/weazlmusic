package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPodCoverPixelsAndLimits(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if x < 50 || x >= 150 {
				c = color.RGBA{0, 0, 255, 255}
			}
			im.SetRGBA(x, y, c)
		}
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, im); err != nil {
		t.Fatal(err)
	}
	pixels, err := coverRGB565(pngData.Bytes())
	if err != nil || len(pixels) != podCoverBytes {
		t.Fatal(len(pixels), err)
	}
	for i := 0; i < len(pixels); i += 2 {
		if pixels[i] != 0 || pixels[i+1] != 0xf8 {
			t.Fatalf("bad center crop or RGB565 byte order at %d", i)
		}
	}
	for _, raw := range [][]byte{[]byte("not an image"), make([]byte, podCoverMaxInput+1), pngData.Bytes()[:20]} {
		if _, err := coverRGB565(raw); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
	progressive, err := os.ReadFile("testdata/pod-cover-progressive.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := coverRGB565(progressive); err != nil || len(got) != podCoverBytes {
		t.Fatal("progressive JPEG", len(got), err)
	}
}

func TestPodCoverLeaseDownloadAndRevocation(t *testing.T) {
	h := setup(t)
	var original bytes.Buffer
	im := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			im.SetRGBA(x, y, color.RGBA{uint8(x * 16), uint8(y * 16), 64, 255})
		}
	}
	if err := png.Encode(&original, im); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getCoverArt.view") {
			switch r.URL.Query().Get("id") {
			case "missing":
				http.NotFound(w, r)
				return
			case "broken":
				w.Write([]byte("bad image"))
				return
			}
			if size := r.URL.Query().Get("size"); size != "" && size != "112" {
				t.Error("unexpected size", size)
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write(original.Bytes())
			return
		}
		h.nav.ServeHTTP(w, r)
	}))
	t.Cleanup(up.Close)
	h.upstream = up.URL
	h.login("alice")
	c := h.native("alice", "cover-pod")
	body := map[string]string{"kind": "cover", "resourceId": "album", "quality": podCoverProfile}
	hint := map[string]string{"X-Weazl-Client": "weazlpod"}
	if code, _, _ := h.vrequest("POST", "media/leases", body, "", hint); code != 401 && code != 403 {
		t.Fatal("anonymous lease", code)
	}
	code, raw, _ := h.vrequest("POST", "media/leases", body, c.Access, hint)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	d := decodeData[map[string]any](t, raw)
	if d["byteLength"] != float64(podCoverBytes) || d["contentType"] != podCoverContentType || d["profile"] != podCoverProfile {
		t.Fatal(d)
	}
	path := d["url"].(string)
	code, pixels, head := podGet(t, h, "GET", path, nil)
	if code != 200 || len(pixels) != podCoverBytes {
		t.Fatal(code, len(pixels))
	}
	hash := sha256.Sum256(pixels)
	if head.Get("X-Weazl-Cover-SHA256") != hex.EncodeToString(hash[:]) {
		t.Fatal("checksum mismatch")
	}
	if code, raw, head := podGet(t, h, "HEAD", path, nil); code != 200 || len(raw) != 0 || head.Get("Content-Length") != "25088" {
		t.Fatal("HEAD", code, head)
	}
	if code, raw, _ := podGet(t, h, "GET", path, map[string]string{"Range": "bytes=2-9"}); code != 206 || !bytes.Equal(raw, pixels[2:10]) {
		t.Fatal("range", code)
	}
	if code, _, _ := podGet(t, h, "GET", path, map[string]string{"If-None-Match": head.Get("ETag")}); code != 304 {
		t.Fatal("ETag", code)
	}
	for _, id := range []string{"missing", "broken"} {
		body["resourceId"] = id
		code, raw, _ = h.vrequest("POST", "media/leases", body, c.Access, hint)
		if code != 200 {
			t.Fatal(code, string(raw))
		}
		d := decodeData[map[string]any](t, raw)
		code, _, _ = podGet(t, h, "GET", d["url"].(string), nil)
		want := 502
		if id == "missing" {
			want = 404
		}
		if code != want {
			t.Fatal(id, code)
		}
	}
	body["resourceId"] = "album"
	body["quality"] = "original"
	code, raw, _ = h.vrequest("POST", "media/leases", body, c.Access, hint)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	d = decodeData[map[string]any](t, raw)
	if code, got, _ := podGet(t, h, "GET", d["url"].(string), nil); code != 200 || !bytes.Equal(got, original.Bytes()) {
		t.Fatal("original cover changed", code)
	}
	body["quality"] = podCoverProfile
	body["kind"] = "trackStream"
	if code, _, _ := h.vrequest("POST", "media/leases", body, c.Access, nil); code != 422 {
		t.Fatal("cover profile accepted for audio", code)
	}
	if code, _, _ := h.vrequest("DELETE", "auth/devices/"+c.Device, nil, c.Access, nil); code != 204 {
		t.Fatal("revoke", code)
	}
	if code, _, _ := podGet(t, h, "GET", path, nil); code != 401 {
		t.Fatal("revoked cover lease", code)
	}
}
