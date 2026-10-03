package server

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func (a *apiV1) finalize(j *recordingJob) {
	m := recordingManifest{Schema: 1, Session: j.ID, Revision: randomID(), Name: j.Name, Starts: j.Starts, Duration: j.Duration, Tracks: []recordingTrack{}}
	gaps := false
	for i, p := range j.Stations {
		segments := p.Segments
		if segments == nil {
			segments = []recordingSegment{}
		}
		track := recordingTrack{p.StationID, p.Station.Name, i, segments, []recordingGap{}, p.Metadata}
		if track.Metadata == nil {
			track.Metadata = []recordingMetadata{}
		}
		cursor := int64(0)
		for _, s := range segments {
			if s.Start > cursor {
				track.Gaps = append(track.Gaps, recordingGap{cursor, s.Start - cursor, "station_unavailable"})
			}
			cursor = max(cursor, s.Start+s.Duration)
			m.Bytes += s.Bytes
		}
		if cursor < j.Duration {
			reason := "station_unavailable"
			if j.StopReason == "user" {
				reason = "stopped"
			}
			track.Gaps = append(track.Gaps, recordingGap{cursor, j.Duration - cursor, reason})
		}
		if len(track.Gaps) > 0 {
			gaps = true
		}
		m.Tracks = append(m.Tracks, track)
		if len(segments) == 0 {
			j.Stations[i].State = "failed"
		} else if len(track.Gaps) > 0 {
			j.Stations[i].State = "partial"
		} else {
			j.Stations[i].State = "complete"
		}
	}
	j.State = "complete"
	if gaps || j.StopReason != "" {
		j.State = "partial"
	}
	if m.Bytes == 0 {
		j.State = "failed"
	}
	m.State = j.State
	j.Bytes = m.Bytes
	j.ManifestRevision = m.Revision
	j.Version++
	mb, e := a.seal(m)
	if e != nil {
		return
	}
	jb, e := a.seal(j)
	if e != nil {
		return
	}
	tx, e := a.db.Begin()
	if e != nil {
		return
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO records VALUES('manifest',?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body", j.ID, accountID(j.Username), mb); e != nil {
		return
	}
	if _, e = tx.Exec("UPDATE records SET body=? WHERE kind='job' AND id=?", jb, j.ID); e != nil {
		return
	}
	if tx.Commit() == nil {
		_ = os.RemoveAll(filepath.Join(a.s.cfg.DataDir, "recordings", "staging", j.ID))
	}
}
func (a *apiV1) deleteRecording(j *recordingJob) error {
	j.Tombstone = true
	j.Version++
	if e := a.put("job", j.ID, accountID(j.Username), j); e != nil {
		return e
	}
	for _, p := range j.Stations {
		for _, s := range p.Segments {
			var ar assetRecord
			if a.get("asset", s.AssetID, accountID(j.Username), &ar) == nil {
				if e := os.Remove(filepath.Join(a.s.cfg.DataDir, "recordings", ar.Path)); e != nil && !os.IsNotExist(e) {
					return e
				}
			}
		}
	}
	return nil
}
func (a *apiV1) retention() {
	days := a.s.cfg.RetentionDays
	if days <= 0 {
		return
	}
	rows, e := a.list("job", "*")
	if e != nil {
		return
	}
	cutoff := a.now().AddDate(0, 0, -days)
	for _, row := range rows {
		var j recordingJob
		if json.Unmarshal(row, &j) == nil && !j.Tombstone && finalState(j.State) && j.Ends.Before(cutoff) {
			_ = a.deleteRecording(&j)
		}
	}
}
