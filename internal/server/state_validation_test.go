package server

import (
	"encoding/json"
	"testing"
)

func TestSavedTrackValidation(t *testing.T) {
	for _, raw := range []string{`null`, `42`, `{"title":"missing ID"}`, `{"id":"a","duration":"oops"}`, `{"id":"a","position":-1}`, `{"id":"a","url":"file:///etc/passwd"}`} {
		if validSavedTrack(json.RawMessage(raw), false) {
			t.Fatalf("accepted invalid track: %s", raw)
		}
	}
	if !validSavedTrack(nil, true) || !validSavedTrack(json.RawMessage(`{"id":"a","title":"A track","duration":120,"position":20}`), false) {
		t.Fatal("valid saved track rejected")
	}
}
