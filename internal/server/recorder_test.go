package server

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRecorderDSTAndOvernightWindows(t *testing.T) {
	cases := []struct {
		After, Start, End string
		Hours             int64
		Correction        bool
	}{{"2026-10-03T12:00:00Z", "22:00", "04:00", 6, false}, {"2026-03-07T12:00:00Z", "22:00", "04:00", 5, true}, {"2026-10-31T12:00:00Z", "22:00", "04:00", 7, true}, {"2026-03-08T05:00:00Z", "02:30", "04:00", 0, true}, {"2026-11-01T04:00:00Z", "01:30", "04:00", 3, true}}
	for _, c := range cases {
		after, _ := time.Parse(time.RFC3339, c.After)
		in := scheduleInput{Zone: "America/New_York", Rule: recurrence{Kind: "daily", LocalStart: c.Start, LocalEnd: c.End}}
		start, end, corrections, e := scheduleWindow(in, after)
		if e != nil {
			t.Fatal(e)
		}
		if int64(end.Sub(start)/time.Hour) != c.Hours {
			t.Fatalf("%s: %s → %s", c.After, start, end)
		}
		if c.Correction && len(corrections) == 0 && c.Hours != 5 && c.Hours != 7 {
			t.Fatal("missing DST correction")
		}
	}
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	if overlapValid([]recordingWindow{{now, now.Add(time.Hour), 6}, {now.Add(time.Minute), now.Add(time.Hour), 1}}) {
		t.Fatal("overlap accepted")
	}
	if !overlapValid([]recordingWindow{{now, now.Add(time.Hour), 6}, {now.Add(time.Hour), now.Add(2 * time.Hour), 6}}) {
		t.Fatal("adjacent windows conflict")
	}
}
func TestRecorderManifestGapTimelineAndStop(t *testing.T) {
	h := setup(t)
	s := h.app.Config.Handler.(*Server)
	a := s.v1
	se := &session{User: "alice"}
	start := time.Date(2026, 10, 4, 2, 0, 0, 0, time.UTC)
	in := scheduleInput{Name: "timeline", Starts: start, Ends: start.Add(time.Hour)}
	stations := []nativeStation{{ID: "first", Name: "First", Preset: true}, {ID: "second", Name: "Second", Preset: true}}
	j := a.makeJob("timeline-job", "", in, se, stations)
	j.StopReason = "user"
	for i := range j.Stations {
		j.Stations[i].Segments = []recordingSegment{{ID: itoa(int64(i)), AssetID: itoa(int64(i)), Start: 0, Duration: 1200000, Bytes: 10, Hash: strings.Repeat("a", 64), ContentType: "audio/mp4", Codec: "aac-lc"}, {ID: "later" + itoa(int64(i)), AssetID: "later" + itoa(int64(i)), Start: 1800000, Duration: 1800000, Bytes: 10, Hash: strings.Repeat("b", 64), ContentType: "audio/mp4", Codec: "aac-lc"}}
	}
	a.mu.Lock()
	_ = a.put("job", j.ID, accountID(se.User), j)
	a.finalize(&j)
	var manifest recordingManifest
	e := a.get("manifest", j.ID, accountID(se.User), &manifest)
	a.mu.Unlock()
	if e != nil || manifest.State != "partial" {
		t.Fatal("finalization", e)
	}
	for _, track := range manifest.Tracks {
		if len(track.Gaps) != 1 || track.Gaps[0].Start != 1200000 || track.Gaps[0].Duration != 600000 {
			t.Fatal("gap compressed timeline")
		}
		for _, timeline := range []int64{1800000, 3000000} {
			seg := track.Segments[1]
			actual := seg.MediaStart + timeline - seg.Start
			if actual != timeline-1800000 {
				t.Fatal("station switch offset changed")
			}
		}
	}
}
func TestRecorderMissedCapacityCancelAndOwnership(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "phone")
	other := h.native("bob", "phone")
	code, b, _ := h.vrequest("GET", "radio/stations", nil, c.Access, nil)
	if code != 200 {
		t.Fatal(code)
	}
	stations := decodeData[[]nativeStation](t, b)
	ids := []string{}
	for _, s := range stations {
		if s.Preset && len(ids) < 6 {
			ids = append(ids, s.ID)
		}
	}
	start := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	body := scheduleInput{Name: "fixture window", StationIDs: ids, Starts: start, Ends: start.Add(time.Hour), Zone: "America/New_York", Rule: recurrence{Kind: "once"}}
	code, b, head := h.vrequest("POST", "flight-recorder/schedules", body, c.Access, nil)
	if code != 201 {
		t.Fatal(code, string(b))
	}
	schedule := decodeData[recordingSchedule](t, b)
	code, b, _ = h.vrequest("POST", "flight-recorder/schedules", body, c.Access, nil)
	if code != 409 || !bytes.Contains(b, []byte("capacity_conflict")) {
		t.Fatal("overlap accepted", code, string(b))
	}
	code, _, _ = h.vrequest("DELETE", "flight-recorder/schedules/"+schedule.ID, nil, other.Access, map[string]string{"If-Match": head.Get("ETag")})
	if code != 404 {
		t.Fatal("foreign schedule accessible")
	}
	code, _, _ = h.vrequest("DELETE", "flight-recorder/schedules/"+schedule.ID, nil, c.Access, map[string]string{"If-Match": head.Get("ETag")})
	if code != 204 {
		t.Fatal("cancel failed", code)
	}
	a := h.app.Config.Handler.(*Server).v1
	a.mu.Lock()
	var cancelled recordingJob
	if a.get("job", schedule.ID+"-"+itoa(start.Unix()), c.Account, &cancelled) != nil || cancelled.State != "cancelled" {
		t.Error("cancelled job will fire")
	}
	se := &session{User: "alice"}
	past := a.makeJob("missed", "", scheduleInput{Starts: time.Now().Add(-2 * time.Hour), Ends: time.Now().Add(-time.Hour)}, se, []nativeStation{{ID: "station"}})
	_ = a.put("job", past.ID, c.Account, past)
	a.mu.Unlock()
	a.tick()
	a.mu.Lock()
	var missed recordingJob
	_ = a.get("job", "missed", c.Account, &missed)
	a.mu.Unlock()
	if missed.State != "missed" {
		t.Fatal("missed window shifted")
	}
}
