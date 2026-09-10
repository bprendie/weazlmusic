package server

import (
	"os"
	"path/filepath"
)

// Restore the first release's settings only after verifying the upstream credentials.
func (st *store) migrateLegacyState(se *session) error {
	if _, err := os.Stat(filepath.Join(st.dir, digest(se.stateKey())+".bin")); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	var previous userState
	err := st.readRecord(se.ServerURL+"/"+se.NavUser, &previous)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return st.write(se.stateKey(), previous)
}
