package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type podStore struct {
	mu       sync.Mutex
	encoder  string
	root     string
	reps     map[string]podRepresentation
	jobs     map[string]*podPreparation
	inflight map[string]string
	encoding map[string]chan struct{}
	readers  map[string]int
	reserved atomic.Int64
	running  int
	live     map[string]int
}
type podPreparation struct {
	ID             string    `json:"preparationId"`
	State          string    `json:"state"`
	Code           string    `json:"errorCode,omitempty"`
	User           string    `json:"-"`
	Library        string    `json:"-"`
	Kind           string    `json:"kind"`
	Resource       string    `json:"resourceId"`
	Representation string    `json:"representationId,omitempty"`
	Created        time.Time `json:"createdAt"`
	Expires        time.Time `json:"expiresAt"`
	cancel         context.CancelFunc
}

func (a *apiV1) initPod() error {
	p := &podStore{root: filepath.Join(a.s.cfg.DataDir, "derivatives"), encoder: podEncoderVersion(), reps: map[string]podRepresentation{}, jobs: map[string]*podPreparation{}, inflight: map[string]string{}, encoding: map[string]chan struct{}{}, readers: map[string]int{}, live: map[string]int{}}
	a.pod = p
	if err := os.MkdirAll(p.root, 0700); err != nil {
		return err
	}
	// Incomplete inputs/outputs are never recovered as playable representations.
	entries, err := os.ReadDir(p.root)
	if err != nil {
		return err
	}
	rows, err := a.list("pod-representation", "*")
	if err != nil {
		return err
	}
	for _, row := range rows {
		var rep podRepresentation
		if json.Unmarshal(row, &rep) != nil {
			return errors.New("invalid derivative record")
		}
		n, h, e := podHash(p.path(rep.ID))
		if e != nil || n != rep.Bytes || h != rep.Hash {
			_ = a.remove("pod-representation", rep.ID)
			continue
		}
		p.reps[rep.ID] = rep
	}
	for _, entry := range entries {
		name := entry.Name()
		if _, ok := p.reps[strings.TrimSuffix(name, ".mp3")]; !ok || filepath.Ext(name) != ".mp3" {
			if err = os.RemoveAll(filepath.Join(p.root, name)); err != nil {
				return err
			}
		}
	}
	// Jobs are bounded ephemeral snapshots. A restart expires their URLs; durable leases survive.
	return nil
}
func (p *podStore) path(id string) string { return filepath.Join(p.root, id+".mp3") }
func (a *apiV1) podBudget() int64 {
	if a.s.cfg.TranscodeBudgetBytes > 0 {
		return a.s.cfg.TranscodeBudgetBytes
	}
	return 4 << 30
}
func (a *apiV1) podWorkers() int {
	if a.s.cfg.TranscodeWorkers > 0 {
		return min(a.s.cfg.TranscodeWorkers, 8)
	}
	return 2
}

// Called with the API admission lock and pod lock held. Recorder reservations win.
func (a *apiV1) podReserve() bool {
	p := a.pod
	var used int64
	for _, rep := range p.reps {
		used += rep.Bytes
	}
	st, err := a.storage()
	if err != nil {
		return false
	}
	// Evict only expendable, unleased files, ordered by last use. Never touch masters.
	candidates := []podRepresentation{}
	for _, rep := range p.reps {
		if !a.now().Before(rep.Pin) && p.readers[rep.ID] == 0 {
			candidates = append(candidates, rep)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Used.Before(candidates[j].Used) })
	for _, rep := range candidates {
		if used+p.reserved.Load()+podReservation <= a.podBudget() && a.podFree(st) >= podReservation {
			break
		}
		if os.Remove(p.path(rep.ID)) != nil {
			continue
		}
		delete(p.reps, rep.ID)
		_ = a.remove("pod-representation", rep.ID)
		used -= rep.Bytes
		st, err = a.storage()
		if err != nil {
			return false
		}
	}
	if used+p.reserved.Load()+podReservation > a.podBudget() || a.podFree(st) < podReservation {
		return false
	}
	p.reserved.Add(podReservation)
	return true
}
func (a *apiV1) podPinLocked(id string, until time.Time) (podRepresentation, bool) {
	p := a.pod
	rep, ok := p.reps[id]
	if !ok {
		return rep, false
	}
	rep.Used = a.now()
	rep.Pin = maxTime(rep.Pin, until)
	if a.put("pod-representation", id, "", rep) != nil {
		return rep, false
	}
	p.reps[id] = rep
	return rep, true
}
func maxTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}
func (a *apiV1) podPublish(ctx context.Context, se *session, source, sourceHash string) (podRepresentation, error) {
	p := a.pod
	id := "mp3-" + digest(a.libraryID(se)+":"+sourceHash+":"+p.encoder)
	p.mu.Lock()
	if rep, ok := a.podPinLocked(id, a.now().Add(time.Hour)); ok {
		p.mu.Unlock()
		return rep, nil
	}
	if wait, ok := p.encoding[id]; ok {
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return podRepresentation{}, ctx.Err()
		case <-wait:
		}
		p.mu.Lock()
		rep, ok := a.podPinLocked(id, a.now().Add(time.Hour))
		p.mu.Unlock()
		if !ok {
			return rep, errors.New("shared encode failed")
		}
		return rep, nil
	}
	done := make(chan struct{})
	p.encoding[id] = done
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.encoding, id); close(done); p.mu.Unlock() }()
	samples, err := podSourceSamples(ctx, source)
	if err != nil {
		return podRepresentation{}, err
	}
	output := source + ".mp3"
	defer os.Remove(output)
	cmd := podEncodeCommand(ctx, source, output)
	if err = cmd.Run(); err != nil {
		return podRepresentation{}, errors.New("encoding failed")
	}
	rep, err := podValidate(ctx, output)
	if err != nil {
		return rep, err
	}
	if delta := rep.Samples - samples; delta > 1 || delta < -1 {
		return rep, errors.New("encoded presentation differs from source")
	}
	rep.ID = id
	rep.SourceHash = sourceHash
	rep.Encoder = p.encoder
	rep.Used = a.now()
	rep.Pin = a.now().Add(time.Hour)
	f, err := os.OpenFile(output, os.O_RDWR, 0600)
	if err != nil {
		return rep, err
	}
	err = f.Sync()
	f.Close()
	if err != nil {
		return rep, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err = os.Rename(output, p.path(id)); err == nil {
		err = syncDir(p.root)
	}
	if err == nil {
		err = a.put("pod-representation", id, "", rep)
	}
	if err != nil {
		_ = os.Remove(p.path(id))
		return rep, err
	}
	p.reps[id] = rep
	return rep, nil
}

// Recording budget exhaustion is separate from filesystem commitments.
func (a *apiV1) podFree(st storageStatus) int64 {
	var fs syscall.Statfs_t
	if syscall.Statfs(a.s.cfg.DataDir, &fs) != nil {
		return 0
	}
	return max(int64(0), int64(fs.Bavail)*int64(fs.Bsize)-st.Reserve-st.Reserved-a.pod.reserved.Load())
}
