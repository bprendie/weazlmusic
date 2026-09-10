package server

import (
	"html"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

type radioMetadata struct {
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	Station string `json:"station,omitempty"`
	Ended   bool   `json:"ended,omitempty"`
}

func metadataText(raw []byte) string {
	text := string(raw)
	if !utf8.Valid(raw) {
		runes := make([]rune, len(raw))
		for i, b := range raw {
			runes[i] = rune(b)
		}
		text = string(runes)
	}
	text = html.UnescapeString(text)
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 512 {
		runes = runes[:512]
	}
	return string(runes)
}
func parseICY(block []byte) (radioMetadata, bool) {
	raw := string(block)
	lower := strings.ToLower(raw)
	start := strings.Index(lower, "streamtitle='")
	if start < 0 {
		return radioMetadata{}, false
	}
	raw = raw[start+len("streamtitle='"):]
	end := strings.Index(raw, "';")
	if end < 0 {
		return radioMetadata{}, false
	}
	title := metadataText([]byte(raw[:end]))
	artist := ""
	if left, right, ok := strings.Cut(title, " - "); ok && strings.TrimSpace(left) != "" && strings.TrimSpace(right) != "" {
		artist = strings.TrimSpace(left)
		title = strings.TrimSpace(right)
	}
	return radioMetadata{Title: title, Artist: artist}, true
}

// ICY inserts one length byte and a padded metadata block after each audio interval.
func relayICY(dst io.Writer, src io.Reader, interval int64, publish func(radioMetadata), flush func()) error {
	for {
		if _, err := io.CopyN(dst, src, interval); err != nil {
			return err
		}
		flush()
		var length [1]byte
		if _, err := io.ReadFull(src, length[:]); err != nil {
			return err
		}
		if length[0] == 0 {
			continue
		}
		block := make([]byte, int(length[0])*16)
		if _, err := io.ReadFull(src, block); err != nil {
			return err
		}
		if meta, ok := parseICY(block); ok {
			publish(meta)
		}
	}
}
