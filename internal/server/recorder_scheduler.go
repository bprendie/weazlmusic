package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

var errSelection = errors.New("choose one to six unique owned presets")

func (a *apiV1) saveScheduleJob(s recordingSchedule, j recordingJob) error {
	sb, e := a.seal(s)
	if e != nil {
		return e
	}
	jb, e := a.seal(j)
	if e != nil {
		return e
	}
	tx, e := a.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, v := range []struct {
		Kind, ID string
		Body     []byte
	}{{"schedule", s.ID, sb}, {"job", j.ID, jb}} {
		if _, e = tx.Exec("INSERT INTO records VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body", v.Kind, v.ID, accountID(s.Username), v.Body); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (a *apiV1) cancelUpcoming(id string) { a.cancelOtherUpcoming(id, "") }
func (a *apiV1) cancelOtherUpcoming(id, except string) {
	rows, e := a.list("job", "*")
	if e != nil {
		return
	}
	for _, row := range rows {
		var j recordingJob
		_ = json.Unmarshal(row, &j)
		if j.ScheduleID == id && j.ID != except && j.State == "scheduled" {
			j.State = "cancelled"
			j.Version++
			_ = a.put("job", j.ID, accountID(j.Username), j)
		}
	}
}
func (a *apiV1) scheduler() {
	defer a.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	a.tick()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.tick()
		}
	}
}
func (a *apiV1) tick() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx.Err() != nil {
		return
	}
	a.cleanupRecordingGarbage(128)
	a.advanceSchedules()
	rows, e := a.list("job", "*")
	if e != nil {
		return
	}
	streams := 0
	for id := range a.active {
		var j recordingJob
		if a.get("job", id, "*", &j) == nil && !a.packaging[id] {
			streams += j.StationCount
		}
	}
	for _, row := range rows {
		var j recordingJob
		if json.Unmarshal(row, &j) != nil || j.Tombstone || finalState(j.State) || a.active[j.ID] != nil {
			continue
		}
		if j.State == "scheduled" && j.Starts.After(a.now()) {
			continue
		}
		if !j.Ends.After(a.now()) && j.State == "scheduled" {
			j.State = "missed"
			j.Version++
			_ = a.put("job", j.ID, accountID(j.Username), j)
			continue
		}
		if j.State != "finalizing" && streams+j.StationCount > 6 {
			continue
		}
		ctx, cancel := context.WithCancel(a.ctx)
		a.active[j.ID] = cancel
		a.packaging[j.ID] = j.State == "finalizing"
		if j.State != "finalizing" {
			streams += j.StationCount
		}
		a.wg.Add(1)
		go a.runJob(ctx, j)
	}
	a.retention()
}
func (a *apiV1) advanceSchedules() {
	rows, e := a.list("schedule", "*")
	if e != nil {
		return
	}
	for _, row := range rows {
		var s recordingSchedule
		if json.Unmarshal(row, &s) != nil || s.State != "active" {
			continue
		}
		currentID := s.ID + "-" + itoa(s.NextStarts.Unix())
		var current recordingJob
		if a.get("job", currentID, accountID(s.Username), &current) != nil {
			continue
		}
		if !finalState(current.State) && !current.Ends.Before(a.now()) {
			continue
		}
		if s.Rule.Kind == "once" {
			s.State = "finished"
			s.Version++
			_ = a.put("schedule", s.ID, accountID(s.Username), s)
			continue
		}
		start, end, corrections, e := scheduleWindow(s.scheduleInput, s.NextStarts.Add(time.Second))
		if e != nil {
			s.State = "finished"
			s.Error = e.Error()
			_ = a.put("schedule", s.ID, accountID(s.Username), s)
			continue
		}
		s.NextStarts, s.NextEnds = start, end
		s.Corrections = corrections
		s.Version++
		in := s.scheduleInput
		in.Starts, in.Ends = start, end
		se := &session{User: s.Username}
		job := a.makeJob(s.ID+"-"+itoa(start.Unix()), s.ID, in, se, s.Snapshots)
		job.Library = s.Library
		// Future occurrences keep their exact boundaries and fail visibly when sources or budget changed.
		var c stationCollection
		valid := a.get("stations", s.StateKey, accountID(s.Username), &c) == nil
		for _, snap := range s.Snapshots {
			found := false
			for _, station := range c.Stations {
				if station.ID == snap.ID && station.URL == snap.URL {
					found = true
				}
			}
			valid = valid && found
		}
		v, storageErr := a.storage()
		userUsed, userReserved, userErr := a.accountStorage(s.Username)
		userAvailable := a.accountBudget() - userUsed - userReserved
		if !valid || end.Sub(start) > 12*time.Hour || storageErr != nil || job.Reserved > min(v.Available, userAvailable) || userErr != nil {
			job.State = "failed"
			job.StopReason = "occurrence_validation_failed"
			s.Error = "Occurrence skipped: station source, duration or storage is invalid."
		}
		if end.Before(a.now()) && job.State == "scheduled" {
			job.State = "missed"
			job.StopReason = "server_downtime"
		}
		_ = a.saveScheduleJob(s, job)
	}
}
func (a *apiV1) runJob(ctx context.Context, initial recordingJob) {
	defer a.wg.Done()
	defer func() { a.mu.Lock(); delete(a.active, initial.ID); delete(a.packaging, initial.ID); a.mu.Unlock() }()
	a.mu.Lock()
	var j recordingJob
	if a.get("job", initial.ID, "*", &j) != nil {
		a.mu.Unlock()
		return
	}
	if j.State != "finalizing" {
		j.State = "recording"
		now := a.now().UTC()
		if j.ActualStarted == nil {
			j.ActualStarted = &now
		}
		j.Version++
		if a.put("job", j.ID, accountID(j.Username), j) != nil {
			a.mu.Unlock()
			return
		}
	}
	a.mu.Unlock()
	for index := range j.Stations {
		root := filepath.Join(a.s.cfg.DataDir, "recordings", "staging", j.ID, j.Stations[index].StationID)
		attempts, _ := os.ReadDir(root)
		for _, entry := range attempts {
			if entry.IsDir() {
				a.recoverAttempt(j, index, filepath.Join(root, entry.Name()))
			}
		}
	}
	if j.State != "finalizing" && j.Ends.After(a.now()) {
		var wg localGroup
		for i := range j.Stations {
			index := i
			wg.goRun(func() { a.captureStation(ctx, j, index) })
		}
		wg.wait()
	}
	if a.ctx.Err() != nil {
		return
	} // Startup resumes remaining windows; shutdown never labels unfinished capture complete.
	a.mu.Lock()
	if a.get("job", j.ID, "*", &j) != nil {
		a.mu.Unlock()
		return
	}
	j.State = "finalizing"
	j.Version++
	if a.put("job", j.ID, accountID(j.Username), j) != nil {
		a.mu.Unlock()
		return
	}
	a.packaging[j.ID] = true // Capture workers have exited; finalization must not consume stream slots.
	a.mu.Unlock()
	replacements := a.compactRecording(a.ctx, &j)
	if a.ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var latest recordingJob
	if a.get("job", j.ID, "*", &latest) != nil || latest.Tombstone || latest.State != "finalizing" {
		return
	}
	// A repeated Stop during packaging must retain the newest version/reason.
	j.Version, j.StopReason = latest.Version, latest.StopReason
	a.finalize(&j, replacements...)
}
