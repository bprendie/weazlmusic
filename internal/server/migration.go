package server

import (
	"os"
	"path/filepath"
)

// migrateGlobalConfig seeds the installation record once from an existing
// account. Existing accounts remain authoritative until the admin-managed
// auth phase switches reads and writes to this record.
func (s *Server) migrateGlobalConfig(a account) error {
	cfg, err := s.store.readGlobal()
	if err == nil {
		if cfg.NavidromeURL != "" || a.Connection == nil {
			return nil
		}
		cfg.NavidromeURL = a.Connection.URL
		if cfg.LLM == nil && a.LLM != nil {
			copyLLM := *a.LLM
			cfg.LLM = &copyLLM
		}
		return s.store.writeGlobal(cfg)
	}
	if !os.IsNotExist(err) {
		return err
	}
	cfg = globalConfig{Version: 1}
	if a.Connection != nil {
		cfg.NavidromeURL = a.Connection.URL
	}
	if a.LLM != nil {
		copyLLM := *a.LLM
		cfg.LLM = &copyLLM
	}
	return s.store.writeGlobal(cfg)
}

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
