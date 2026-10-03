package server

import (
	"encoding/json"
	"net/http"
)

func (a *apiV1) recorderRead(w http.ResponseWriter, r *http.Request, se *session, p string, parts []string) {
	a.mu.Lock()
	if p == "storage" {
		v, e := a.storage()
		used, reserved, accountErr := a.accountStorage(se.User)
		if accountErr != nil {
			e = accountErr
		}
		v.AccountUsed = used
		v.AccountBudget = a.accountBudget()
		v.AccountAvailable = max(int64(0), min(v.Available, v.AccountBudget-used-reserved))
		a.mu.Unlock()
		if e != nil {
			verror(w, 503, "storage_full", "Could not inspect recording storage.")
			return
		}
		dataOut(w, 200, v)
		return
	}
	if p == "schedules" || p == "sessions" {
		kind := "schedule"
		if p == "sessions" {
			kind = "job"
		}
		rows, e := a.list(kind, accountID(se.User))
		a.mu.Unlock()
		if e != nil {
			verror(w, 503, "storage_full", "Could not read recordings.")
			return
		}
		out := []any{}
		for _, row := range rows {
			if kind == "job" {
				var j recordingJob
				_ = json.Unmarshal(row, &j)
				if j.Tombstone || r.URL.Query().Get("state") != "" && r.URL.Query().Get("state") != j.State {
					continue
				}
				out = append(out, j)
			} else {
				var s recordingSchedule
				_ = json.Unmarshal(row, &s)
				out = append(out, s)
			}
		}
		a.page(w, r, se, out)
		return
	}
	if len(parts) >= 2 && parts[0] == "sessions" {
		var j recordingJob
		e := a.get("job", parts[1], accountID(se.User), &j)
		if e != nil || j.Tombstone {
			a.mu.Unlock()
			verror(w, 404, "not_found", "Recording not found.")
			return
		}
		if len(parts) == 3 && parts[2] == "manifest" {
			if j.ManifestRevision == "" {
				a.mu.Unlock()
				verror(w, 409, "version_conflict", "Manifest is not finalized yet.")
				return
			}
			var m recordingManifest
			e = a.get("manifest", j.ID, accountID(se.User), &m)
			a.mu.Unlock()
			if e != nil {
				verror(w, 503, "storage_full", "Manifest is unavailable.")
				return
			}
			tag := `"` + m.Revision + `"`
			w.Header().Set("ETag", tag)
			if r.Header.Get("If-None-Match") == tag {
				w.WriteHeader(304)
				return
			}
			dataOut(w, 200, m)
			return
		}
		a.mu.Unlock()
		w.Header().Set("ETag", etag(j.Version))
		dataOut(w, 200, j)
		return
	}
	if len(parts) == 2 && parts[0] == "schedules" {
		var s recordingSchedule
		e := a.get("schedule", parts[1], accountID(se.User), &s)
		a.mu.Unlock()
		if e != nil {
			verror(w, 404, "not_found", "Schedule not found.")
			return
		}
		w.Header().Set("ETag", etag(s.Version))
		dataOut(w, 200, s)
		return
	}
	a.mu.Unlock()
	verror(w, 404, "not_found", "Unknown recorder endpoint.")
	return

}
