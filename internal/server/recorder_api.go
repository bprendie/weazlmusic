package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (a *apiV1) selection(se *session, ids []string) ([]nativeStation, error) {
	c, e := a.collection(se)
	if e != nil {
		return nil, e
	}
	out := []nativeStation{}
	selected := map[string]bool{}
	for _, id := range ids {
		if selected[id] {
			return nil, errSelection
		}
		selected[id] = true
	}
	if len(selected) < 1 || len(selected) > 6 {
		return nil, errSelection
	}
	for _, s := range c.Stations {
		if selected[s.ID] && s.Preset {
			out = append(out, s)
		}
	}
	if len(out) != len(ids) {
		return nil, errSelection
	}
	return out, nil
}
func (a *apiV1) makeJob(id, schedule string, in scheduleInput, se *session, snapshots []nativeStation) recordingJob {
	tracks := []stationProgress{}
	for _, s := range snapshots {
		tracks = append(tracks, stationProgress{Station: s, StationID: s.ID, State: "scheduled", Segments: []recordingSegment{}, Metadata: []recordingMetadata{}})
	}
	return recordingJob{ID: id, ScheduleID: schedule, Name: in.Name, State: "scheduled", Username: se.User, Library: a.libraryID(se), Starts: in.Starts, Ends: in.Ends, Duration: in.Ends.Sub(in.Starts).Milliseconds(), StationCount: len(tracks), Stations: tracks, Version: 1, Reserved: estimateBytes(in.Ends.Sub(in.Starts), len(tracks))}
}
func (a *apiV1) recorder(w http.ResponseWriter, r *http.Request, se *session) {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/flight-recorder/")
	parts := strings.Split(p, "/")
	if r.Method == "GET" {
		a.recorderRead(w, r, se, p, parts)
		return
	}

	if len(parts) >= 2 && parts[0] == "sessions" {
		var j recordingJob
		if a.get("job", parts[1], accountID(se.User), &j) != nil || j.Tombstone {
			verror(w, 404, "not_found", "Recording not found.")
			return
		}
		if r.Method == "POST" && len(parts) == 3 && parts[2] == "stop" {
			if !finalState(j.State) {
				j.StopReason = "user"
				if j.State == "scheduled" && j.Starts.After(a.now()) {
					j.State = "cancelled"
				} else {
					j.State = "finalizing"
				}
				j.Version++
				if a.put("job", j.ID, accountID(se.User), j) != nil {
					verror(w, 503, "storage_full", "Could not stop recording.")
					return
				}
				if cancel := a.active[j.ID]; cancel != nil {
					cancel()
				}
			}
			dataOut(w, 202, j)
			return
		}
		if r.Method == "DELETE" && len(parts) == 2 {
			if !precondition(w, r, j.Version) {
				return
			}
			if !finalState(j.State) {
				verror(w, 409, "version_conflict", "Stop this recording before deleting it.")
				return
			}
			if e := a.deleteRecording(&j); e != nil {
				verror(w, 503, "storage_full", "Could not remove recording.")
				return
			}
			w.WriteHeader(204)
			return
		}
		verror(w, 405, "unsupported", "Unsupported recording operation.")
		return
	}
	if len(parts) == 2 && parts[0] == "schedules" && r.Method == "DELETE" {
		var s recordingSchedule
		if a.get("schedule", parts[1], accountID(se.User), &s) != nil {
			verror(w, 404, "not_found", "Schedule not found.")
			return
		}
		if !precondition(w, r, s.Version) {
			return
		}
		s.State = "cancelled"
		s.Version++
		if a.put("schedule", s.ID, accountID(se.User), s) != nil {
			verror(w, 503, "storage_full", "Could not cancel schedule.")
			return
		}
		a.cancelUpcoming(s.ID)
		w.WriteHeader(204)
		return
	}
	if !a.mediaEnabled {
		verror(w, 503, "unsupported", "Recording tools are unavailable on this installation.")
		return
	}
	if p == "sessions" && r.Method == "POST" {
		var in struct {
			Name     string   `json:"name"`
			IDs      []string `json:"stationIds"`
			Duration int64    `json:"durationMs"`
		}
		if decode(r, &in) != nil {
			verror(w, 422, "validation_failed", "Invalid recording request.")
			return
		}
		if in.Duration == 0 {
			in.Duration = 14400000
		}
		snap, e := a.selection(se, in.IDs)
		if e != nil || in.Duration < 1000 || in.Duration > 43200000 || len(in.Name) > 200 {
			verror(w, 422, "validation_failed", "Choose one to six saved presets and a duration up to twelve hours.")
			return
		}
		if in.Name == "" {
			in.Name = "Flight recording"
		}
		now := a.now().UTC()
		s := scheduleInput{Name: in.Name, StationIDs: in.IDs, Starts: now, Ends: now.Add(time.Duration(in.Duration) * time.Millisecond), Rule: recurrence{Kind: "once"}}
		if !a.checkReservation(w, s, len(snap), "", se.User) {
			return
		}
		job := a.makeJob(randomID(), "", s, se, snap)
		if a.put("job", job.ID, accountID(se.User), job) != nil {
			verror(w, 503, "storage_full", "Could not save recording.")
			return
		}
		w.Header().Set("ETag", etag(job.Version))
		dataOut(w, 201, job)
		return
	}
	if !(p == "schedules" && r.Method == "POST" || len(parts) == 2 && parts[0] == "schedules" && r.Method == "PATCH") {
		verror(w, 404, "not_found", "Unknown recorder mutation.")
		return
	}
	var in scheduleInput
	var schedule recordingSchedule
	if len(parts) == 2 {
		if a.get("schedule", parts[1], accountID(se.User), &schedule) != nil {
			verror(w, 404, "not_found", "Schedule not found.")
			return
		}
		if !precondition(w, r, schedule.Version) {
			return
		}
		in = schedule.scheduleInput
	}
	// PATCH merges explicitly supplied fields through the existing request shape.
	if r.Method == "PATCH" {
		var patch map[string]json.RawMessage
		if decode(r, &patch) != nil {
			verror(w, 422, "validation_failed", "Invalid schedule edit.")
			return
		}
		b, _ := json.Marshal(in)
		var merged map[string]json.RawMessage
		_ = json.Unmarshal(b, &merged)
		for k, v := range patch {
			if _, ok := merged[k]; !ok {
				verror(w, 422, "validation_failed", "Unknown schedule field.")
				return
			}
			merged[k] = v
		}
		b, _ = json.Marshal(merged)
		if json.Unmarshal(b, &in) != nil {
			verror(w, 422, "validation_failed", "Invalid schedule fields.")
			return
		}
	} else if decode(r, &in) != nil {
		verror(w, 422, "validation_failed", "Invalid schedule.")
		return
	}
	if in.Rule.Kind == "" {
		in.Rule.Kind = "once"
	}
	if _, e := time.LoadLocation(in.Zone); e != nil {
		verror(w, 422, "validation_failed", "Choose a valid IANA time zone.")
		return
	}
	start, end, corrections, e := scheduleWindow(in, a.now())
	snap, selectErr := a.selection(se, in.StationIDs)
	if e != nil || selectErr != nil || !start.After(a.now()) || end.Sub(start) > 12*time.Hour || end.Sub(start) < time.Second || strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 {
		verror(w, 422, "validation_failed", "Choose future start/end times, one to six saved presets, and a window up to twelve hours.")
		return
	}
	in.Starts, in.Ends = start, end
	if !a.checkReservation(w, in, len(snap), schedule.ID, se.User) {
		return
	}
	if schedule.ID == "" {
		schedule.ID = randomID()
		schedule.Version = 0
	}
	schedule.Version++
	schedule.scheduleInput = in
	schedule.State = "active"
	schedule.Username = se.User
	schedule.Library = a.libraryID(se)
	schedule.StateKey = se.stateKey()
	schedule.NextStarts, schedule.NextEnds = start, end
	schedule.Snapshots = snap
	schedule.Corrections = corrections
	job := a.makeJob(schedule.ID+"-"+itoa(start.Unix()), schedule.ID, in, se, snap)
	if e = a.saveScheduleJob(schedule, job); e != nil {
		verror(w, 503, "storage_full", "Could not save schedule and reservation.")
		return
	}
	a.cancelOtherUpcoming(schedule.ID, job.ID)
	w.Header().Set("ETag", etag(schedule.Version))
	code := 200
	if r.Method == "POST" {
		code = 201
	}
	dataOut(w, code, schedule)
}
func (a *apiV1) checkReservation(w http.ResponseWriter, in scheduleInput, count int, exclude, user string) bool {
	if !a.capacity(in, count, exclude) {
		verror(w, 409, "capacity_conflict", "This window exceeds six simultaneous capture streams.")
		return false
	}
	v, e := a.storage()
	userUsed, userReserved, userErr := a.accountStorage(user)
	userAvailable := a.accountBudget() - userUsed - userReserved
	if exclude != "" {
		rows, err := a.list("job", "*")
		if err == nil {
			for _, row := range rows {
				var j recordingJob
				if json.Unmarshal(row, &j) == nil && j.ScheduleID == exclude && j.State == "scheduled" {
					v.Available += j.Reserved
					userAvailable += j.Reserved
				}
			}
		}
	}
	if e != nil || userErr != nil || estimateBytes(in.Ends.Sub(in.Starts), count) > min(v.Available, userAvailable) {
		verror(w, 409, "storage_full", "There is not enough recording storage for this window.")
		return false
	}
	return true
}
