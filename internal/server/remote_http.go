package server

import (
	"net/http"
	"strings"
	"time"
)

// Remote operations use their own short critical sections, never generic mutation's
// global API mutex. All remote data is encrypted by the existing record store.
func (a *apiV1) remoteHTTP(w http.ResponseWriter, r *http.Request, se *session) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/remote/")
	if r.Method == "GET" && (path == "events" || path == "receiver/events") {
		a.remoteEvents(w, r, se, path == "receiver/events")
		return
	}
	rs := a.remote
	rs.mu.Lock()
	defer rs.mu.Unlock()
	owner := accountID(se.User)
	library := a.libraryID(se)
	if r.Method != "GET" {
		rateKey := owner
		if strings.HasPrefix(path, "receiver/") {
			rateKey += se.DeviceID
		}
		rate := rs.rates[rateKey]
		if a.now().Sub(rate.At) >= time.Second {
			rate = remoteRate{At: a.now()}
		}
		rate.Count++
		rs.rates[rateKey] = rate
		if rate.Count > 20 {
			w.Header().Set("Retry-After", "1")
			verror(w, 429, "rate_limited", "Too many player updates.")
			return
		}
	}
	a.remoteCleanup()
	if path == "players" && r.Method == "GET" {
		dataOut(w, 200, a.remotePlayers(owner, library))
		return
	}
	if strings.HasPrefix(path, "players/") {
		bits := strings.Split(path, "/")
		p := rs.players[bits[1]]
		if p == nil || p.Owner != owner || p.Library != library || !a.remoteDeviceValid(p) {
			verror(w, 404, "not_found", "Player not found.")
			return
		}
		if len(bits) == 2 && r.Method == "GET" {
			p.Online = a.remoteOnline(p)
			w.Header().Set("ETag", etag(p.Revision))
			dataOut(w, 200, p)
			return
		}
		if len(bits) == 3 && bits[2] == "commands" && r.Method == "POST" {
			a.remoteSubmit(w, r, se, p)
			return
		}
		if len(bits) == 4 && bits[2] == "commands" && r.Method == "GET" {
			c := rs.commands[bits[3]]
			if c == nil || c.Player != p.ID {
				verror(w, 404, "not_found", "Command not found.")
				return
			}
			dataOut(w, 200, c)
			return
		}
	}
	if !strings.HasPrefix(path, "receiver") || se.DeviceID == "" {
		verror(w, 403, "forbidden", "Native receiver credentials required.")
		return
	}
	p := rs.players[se.DeviceID]
	if path == "receiver" && r.Method == "PUT" {
		var in struct {
			Name     string   `json:"name"`
			Kinds    []string `json:"sourceKinds"`
			Commands []string `json:"commands"`
		}
		if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 || len(in.Kinds) == 0 || len(in.Kinds) > 2 || len(in.Commands) > len(remoteCommands) || !uuidKey.MatchString(r.Header.Get("Idempotency-Key")) {
			verror(w, 422, "validation_failed", "Invalid receiver registration.")
			return
		}
		for _, s := range in.Kinds {
			if !remoteContains([]string{"library", "radioLive"}, s) {
				verror(w, 422, "unsupported", "Unsupported source.")
				return
			}
		}
		for _, s := range in.Commands {
			if !remoteContains(remoteCommands, s) {
				verror(w, 422, "unsupported", "Unsupported control.")
				return
			}
		}
		if p == nil {
			count := 0
			for _, v := range rs.players {
				if v.Owner == owner && v.Library == library && a.remoteDeviceValid(v) {
					count++
				}
			}
			if count >= 16 {
				verror(w, 409, "capacity_conflict", "Receiver limit reached.")
				return
			}
			p = &remotePlayer{ID: se.DeviceID, Owner: owner, Library: library, Epoch: rs.epoch, Revision: 1, State: remoteState{Transport: "idle", Volume: .7}}
		}
		if p.Library != library {
			verror(w, 409, "version_conflict", "Library changed; enroll a new device.")
			return
		}
		copy := *p
		copy.Name = in.Name
		copy.Kinds = in.Kinds
		copy.Commands = in.Commands
		if a.remoteSavePlayer(&copy) != nil {
			verror(w, 503, "storage_full", "Could not save receiver.")
			return
		}
		rs.players[p.ID] = &copy
		dataOut(w, 200, &copy)
		return
	}
	if p == nil || p.Owner != owner || p.Library != library {
		verror(w, 404, "not_found", "Register receiver first.")
		return
	}
	if path == "receiver/connections" && r.Method == "POST" {
		key := r.Header.Get("Idempotency-Key")
		if !uuidKey.MatchString(key) {
			verror(w, 422, "validation_failed", "UUID required.")
			return
		}
		// Connection keys are generation identifiers, making identical retries stable.
		connection := digest(rs.epoch + se.DeviceID + key)
		if p.Connection != connection {
			p.Connection = connection
			p.Sequence = 0
			p.reportHash = ""
			p.Seen = time.Time{}
			p.attached = false
			p.Revision++
			p.Epoch = rs.epoch
			for _, c := range rs.commands {
				if c.Player == p.ID && (c.Status == "queued" || c.Status == "delivered" || c.Status == "outcome_unknown") {
					c.Status = "outcome_unknown"
					if a.remoteSaveCommand(c, owner) != nil {
						verror(w, 503, "storage_full", "Could not fence commands.")
						return
					}
				}
			}
		}
		dataOut(w, 200, p)
		return
	}
	if path == "receiver/reports" && r.Method == "POST" {
		var in struct {
			Connection string      `json:"connectionId"`
			Sequence   int64       `json:"sequence"`
			State      remoteState `json:"state"`
		}
		if decode(r, &in) != nil || !remoteValidState(in.State) || in.Sequence < 1 {
			verror(w, 422, "validation_failed", "Invalid report.")
			return
		}
		hash := remoteHash(in)
		if p.Connection == "" || in.Connection != p.Connection || in.Sequence < p.Sequence || in.Sequence == p.Sequence && hash != p.reportHash || in.State.Revision < p.State.Revision {
			verror(w, 409, "version_conflict", "Stale receiver report.")
			return
		}
		if in.Sequence == p.Sequence {
			dataOut(w, 200, p)
			return
		}
		if in.State.Revision == p.State.Revision && remoteDiscreteHash(in.State) != remoteDiscreteHash(p.State) {
			verror(w, 409, "version_conflict", "Discrete changes require a new receiver revision.")
			return
		}
		if in.State.Revision != p.State.Revision {
			p.Revision++
		}
		p.Sequence = in.Sequence
		p.reportHash = hash
		p.State = in.State
		p.Seen = a.now()
		for _, c := range rs.commands {
			if c.Player == p.ID && c.Connection == p.Connection && (c.Status == "queued" || c.Status == "delivered" || c.Status == "outcome_unknown") {
				if in.State.Applied == c.ID && in.State.Revision > c.Revision && remoteAppliedMatches(c, in.State) {
					c.Status = "applied"
				} else if in.State.Applied != c.ID && in.State.Revision > c.Revision {
					c.Status = "rejected"
				}
				if a.remoteSaveCommand(c, owner) != nil {
					verror(w, 503, "storage_full", "Could not save command result.")
					return
				}
			}
		}
		dataOut(w, 200, p)
		return
	}
	bits := strings.Split(path, "/")
	if len(bits) == 4 && bits[0] == "receiver" && bits[1] == "commands" && bits[3] == "ack" && r.Method == "POST" {
		c := rs.commands[bits[2]]
		var in struct {
			Connection string      `json:"connectionId"`
			Status     string      `json:"status"`
			State      remoteState `json:"state"`
		}
		if decode(r, &in) != nil || !remoteContains([]string{"applied", "rejected"}, in.Status) || !remoteValidState(in.State) {
			verror(w, 422, "validation_failed", "Invalid acknowledgement.")
			return
		}
		if c == nil || c.Player != p.ID {
			verror(w, 404, "not_found", "Command not found.")
			return
		}
		hash := remoteHash(in)
		if c.AckHash == hash {
			dataOut(w, 200, c)
			return
		}
		if (c.Status == "rejected" && in.Status == "applied") || c.AckHash != "" || in.Connection != p.Connection || c.Connection != p.Connection || p.Connection == "" || in.State.Revision < p.State.Revision || in.Status == "applied" && (in.State.Applied != c.ID || in.State.Revision <= c.Revision || !remoteAppliedMatches(c, in.State)) {
			verror(w, 409, "version_conflict", "Stale or conflicting acknowledgement.")
			return
		}
		copy := *c
		copy.AckHash = hash
		copy.Status = in.Status
		if a.remoteSaveCommand(&copy, owner) != nil {
			verror(w, 503, "storage_full", "Could not save result.")
			return
		}
		*c = copy
		p.State = in.State
		p.Seen = a.now()
		p.Revision++
		dataOut(w, 200, c)
		return
	}
	verror(w, 404, "not_found", "Unknown remote endpoint.")
}
