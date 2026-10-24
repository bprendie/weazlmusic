package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	podCoverProfile     = "rgb565-112"
	podCoverContentType = "application/vnd.weazlpod.rgb565"
	podCoverWidth       = 112
	podCoverBytes       = podCoverWidth * podCoverWidth * 2
	podCoverMaxInput    = 4 * 1024 * 1024
	podCoverMaxPixels   = 16 * 1024 * 1024
)

// Bound decode memory even if an upstream ignores its thumbnail-size hint.
var podCoverSlots = make(chan struct{}, 2)

func coverRGB565(raw []byte) ([]byte, error) {
	if len(raw) > podCoverMaxInput {
		return nil, errors.New("cover input too large")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > podCoverMaxPixels {
		return nil, errors.New("cover dimensions out of bounds")
	}
	im, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	// Center-crop to a square, then use bilinear samples. Alpha is composited
	// over black by RGBA's premultiplied channel values.
	b := im.Bounds()
	side := min(b.Dx(), b.Dy())
	ox, oy := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	out := make([]byte, podCoverBytes)
	for y := 0; y < podCoverWidth; y++ {
		fy := max(0.0, (float64(y)+0.5)*float64(side)/podCoverWidth-0.5)
		y0 := min(int(fy), side-1)
		y1 := min(y0+1, side-1)
		wy := fy - float64(y0)
		for x := 0; x < podCoverWidth; x++ {
			fx := max(0.0, (float64(x)+0.5)*float64(side)/podCoverWidth-0.5)
			x0 := min(int(fx), side-1)
			x1 := min(x0+1, side-1)
			wx := fx - float64(x0)
			r0, g0, b0, _ := im.At(ox+x0, oy+y0).RGBA()
			r1, g1, b1, _ := im.At(ox+x1, oy+y0).RGBA()
			r2, g2, b2, _ := im.At(ox+x0, oy+y1).RGBA()
			r3, g3, b3, _ := im.At(ox+x1, oy+y1).RGBA()
			blend := func(a, b, c, d uint32) uint16 {
				v := (float64(a)*(1-wx)+float64(b)*wx)*(1-wy) + (float64(c)*(1-wx)+float64(d)*wx)*wy
				return uint16(v + 0.5)
			}
			r, g, blue := blend(r0, r1, r2, r3), blend(g0, g1, g2, g3), blend(b0, b1, b2, b3)
			pixel := (r>>11)<<11 | (g>>10)<<5 | blue>>11
			binary.LittleEndian.PutUint16(out[(y*podCoverWidth+x)*2:], pixel)
		}
	}
	return out, nil
}

func (a *apiV1) podCover(w http.ResponseWriter, r *http.Request, se *session, ml mediaLease) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	select {
	case podCoverSlots <- struct{}{}:
		defer func() { <-podCoverSlots }()
	default:
		w.Header().Set("Retry-After", "1")
		verror(w, 429, "busy", "Thumbnail workers are busy; retry shortly.")
		return
	}
	q := url.Values{"id": {ml.Resource}, "size": {"112"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.s.endpoint(se, "getCoverArt", q), nil)
	if err != nil {
		verror(w, 502, "upstream_unavailable", "Cover is unavailable.")
		return
	}
	res, err := a.s.upstream.Do(req)
	if err != nil {
		verror(w, 502, "upstream_unavailable", "Cover is unavailable.")
		return
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		verror(w, 404, "not_found", "Cover is unavailable.")
		return
	}
	if res.StatusCode != http.StatusOK {
		verror(w, 502, "upstream_rejected", "Cover request failed.")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, podCoverMaxInput+1))
	if err != nil {
		verror(w, 502, "upstream_unavailable", "Cover transfer failed.")
		return
	}
	pixels, err := coverRGB565(raw)
	if err != nil {
		verror(w, 502, "invalid_cover", "Cover could not be decoded within the thumbnail limits.")
		return
	}
	hash := sha256.Sum256(pixels)
	digest := hex.EncodeToString(hash[:])
	w.Header().Set("Content-Type", podCoverContentType)
	w.Header().Set("X-Weazl-Cover-SHA256", digest)
	w.Header().Set("ETag", `"`+digest+`"`)
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	http.ServeContent(w, r, "cover.rgb565", time.Time{}, bytes.NewReader(pixels))
}
