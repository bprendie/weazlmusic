package server

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
)

type recordingGarbage struct {
	ID    string   `json:"id"`
	Owner string   `json:"owner"`
	Paths []string `json:"paths"`
}

func (a *apiV1) commitRecordingReplacements(tx *sql.Tx, j *recordingJob, replacements []recordingReplacement) error {
	garbage := recordingGarbage{ID: j.ID, Owner: accountID(j.Username)}
	for _, replacement := range replacements {
		body, err := a.seal(replacement.Asset)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO records VALUES('asset',?,?,?)", replacement.Asset.ID, garbage.Owner, body); err != nil {
			return err
		}
		for _, old := range replacement.Old {
			if _, err = tx.Exec("DELETE FROM records WHERE kind='asset' AND id=? AND owner=?", old.AssetID, garbage.Owner); err != nil {
				return err
			}
			garbage.Paths = append(garbage.Paths, old.AssetID+".m4a")
		}
	}
	if len(garbage.Paths) == 0 {
		return nil
	}
	body, err := a.seal(garbage)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO records VALUES('recording-garbage',?,?,?)", j.ID, garbage.Owner, body)
	return err
}

// Called under the API lock. A durable journal makes unlink-after-commit safe
// across crashes, without deleting source audio before its replacement commits.
// Bound work per tick so a large completed flight does not block API requests.
func (a *apiV1) cleanupRecordingGarbage(limit int) {
	rows, err := a.list("recording-garbage", "*")
	if err != nil {
		return
	}
	for _, row := range rows {
		var garbage recordingGarbage
		if json.Unmarshal(row, &garbage) != nil {
			continue
		}
		for len(garbage.Paths) > 0 && limit > 0 {
			path := garbage.Paths[0]
			if filepath.Base(path) != path {
				return
			}
			err = os.Remove(filepath.Join(a.s.cfg.DataDir, "recordings", path))
			if err != nil && !os.IsNotExist(err) {
				break
			}
			garbage.Paths = garbage.Paths[1:]
			limit--
		}
		if len(garbage.Paths) == 0 {
			_, _ = a.db.Exec("DELETE FROM records WHERE kind='recording-garbage' AND id=?", garbage.ID)
		} else {
			_ = a.put("recording-garbage", garbage.ID, garbage.Owner, garbage)
		}
		if limit == 0 {
			return
		}
	}
}
