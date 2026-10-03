package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeDirectoryPagingDoesNotTruncateIcecast(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "directory")
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<directory>")
		for i := 0; i < 241; i++ {
			fmt.Fprintf(w, "<entry><server_name>Fixture %03d</server_name><listen_url>https://radio.example.invalid/%d</listen_url></entry>", i, i)
		}
		fmt.Fprint(w, "</directory>")
	}))
	h.app.Config.Handler.(*Server).radio = &http.Client{Transport: fixtureRadioTransport{directory.Client(), directory.URL}}
	code, raw, _ := h.vrequest("GET", "radio/directory?provider=icecast&limit=200", nil, c.Access, nil)
	if code != 200 || len(decodeData[[]map[string]any](t, raw)) != 200 {
		t.Fatal("directory truncated", code, string(raw))
	}
	var page struct {
		Cursor string `json:"nextCursor"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	directory.Close()
	code, raw, _ = h.vrequest("GET", "radio/directory?provider=icecast&limit=200&cursor="+page.Cursor, nil, c.Access, nil)
	if code != 200 || len(decodeData[[]map[string]any](t, raw)) != 41 || !strings.Contains(string(raw), "Fixture 240") {
		t.Fatal("directory snapshot requires upstream or truncates", code, string(raw))
	}
}
