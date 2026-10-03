package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

const contractRevision = "2026-10-03.2"

type apiV1 struct {
	s            *Server
	db           *sql.DB
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	lock         *os.File
	installation string
	now          func() time.Time
	active       map[string]context.CancelFunc
	moodCancel   map[string]context.CancelFunc
	snapshot     map[string]*librarySnapshot
	mediaEnabled bool
}

func newV1(s *Server) (*apiV1, error) {
	lock, err := os.OpenFile(filepath.Join(s.cfg.DataDir, "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("data volume is already in use")
	}
	db, err := sql.Open("sqlite", filepath.Join(s.cfg.DataDir, "weazltunes.db"))
	if err != nil {
		lock.Close()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var schemaVersion int
	if err = db.QueryRow("PRAGMA user_version").Scan(&schemaVersion); err != nil || schemaVersion > 1 {
		db.Close()
		lock.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("data schema is newer than this server")
	}
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS records(kind TEXT NOT NULL,id TEXT NOT NULL,owner TEXT NOT NULL,body BLOB NOT NULL,PRIMARY KEY(kind,id)); PRAGMA user_version=1;`)
	if err != nil {
		db.Close()
		lock.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := s.cfg.Now
	if now == nil {
		now = time.Now
	}
	a := &apiV1{s: s, db: db, lock: lock, ctx: ctx, cancel: cancel, now: now, active: map[string]context.CancelFunc{}, snapshot: map[string]*librarySnapshot{}}
	if err = a.get("meta", "installation", "", &a.installation); errors.Is(err, sql.ErrNoRows) {
		a.installation = randomID()
		err = a.put("meta", "installation", "", a.installation)
	}
	if err != nil {
		a.close()
		return nil, err
	}
	a.recoverMoods()
	a.mediaEnabled = mediaToolsAvailable()
	if err = os.MkdirAll(filepath.Join(s.cfg.DataDir, "recordings"), 0700); err != nil {
		a.close()
		return nil, err
	}
	if !s.cfg.DisableWorkers {
		a.wg.Add(1)
		go a.scheduler()
	}
	return a, nil
}
func (a *apiV1) close() {
	a.cancel()
	a.wg.Wait()
	a.db.Close()
	syscall.Flock(int(a.lock.Fd()), syscall.LOCK_UN)
	a.lock.Close()
}
func (a *apiV1) seal(v any) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	n := make([]byte, a.s.store.aead.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return nil, e
	}
	return a.s.store.aead.Seal(n, n, b, []byte("api-v1")), nil
}
func (a *apiV1) open(b []byte, v any) error {
	n := a.s.store.aead.NonceSize()
	if len(b) < n {
		return errors.New("damaged record")
	}
	p, e := a.s.store.aead.Open(nil, b[:n], b[n:], []byte("api-v1"))
	if e != nil {
		return e
	}
	return json.Unmarshal(p, v)
}
func (a *apiV1) put(kind, id, owner string, v any) error {
	b, e := a.seal(v)
	if e != nil {
		return e
	}
	_, e = a.db.Exec("INSERT INTO records VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET owner=excluded.owner,body=excluded.body", kind, id, owner, b)
	return e
}
func (a *apiV1) get(kind, id, owner string, v any) error {
	var b []byte
	var own string
	e := a.db.QueryRow("SELECT owner,body FROM records WHERE kind=? AND id=?", kind, id).Scan(&own, &b)
	if e != nil {
		return e
	}
	if owner != "*" && owner != own {
		return sql.ErrNoRows
	}
	return a.open(b, v)
}
func (a *apiV1) list(kind, owner string) ([]json.RawMessage, error) {
	q := "SELECT body FROM records WHERE kind=?"
	args := []any{kind}
	if owner != "*" {
		q += " AND owner=?"
		args = append(args, owner)
	}
	q += " ORDER BY id"
	rows, e := a.db.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		var v json.RawMessage
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = a.open(b, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (a *apiV1) remove(kind, id string) error {
	_, e := a.db.Exec("DELETE FROM records WHERE kind=? AND id=?", kind, id)
	return e
}
func accountID(user string) string { return "account-" + digest(user)[:24] }
func (a *apiV1) libraryID(se *session) string {
	if se.ServerURL == "" {
		return ""
	}
	return "library-" + digest(a.installation + ":" + se.ServerURL + "/" + se.NavUser)[:24]
}
