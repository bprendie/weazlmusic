package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (a *apiV1) remoteEvents(w http.ResponseWriter, r *http.Request, se *session, receiver bool) {
	rs := a.remote
	owner := accountID(se.User)
	library := a.libraryID(se)
	connection := r.URL.Query().Get("connectionId")
	key := owner
	if receiver {
		key = "receiver:" + se.DeviceID
	}
	rs.mu.Lock()
	if receiver {
		p := rs.players[se.DeviceID]
		if se.DeviceID == "" || p == nil || p.Owner != owner || p.Library != library || connection == "" || connection != p.Connection {
			rs.mu.Unlock()
			verror(w, 403, "forbidden", "Current receiver connection required.")
			return
		}
		if rs.streams[key] != 0 {
			rs.mu.Unlock()
			verror(w, 409, "player_busy", "Receiver already connected.")
			return
		}
		p.attached = true
	} else if rs.streams[key] >= 8 {
		rs.mu.Unlock()
		verror(w, 429, "rate_limited", "Too many controller connections.")
		return
	}
	var revoked <-chan struct{}
	if se.DeviceID != "" {
		ch := rs.revoked[se.DeviceID]
		if ch == nil {
			ch = make(chan struct{})
			rs.revoked[se.DeviceID] = ch
		}
		revoked = ch
	}
	rs.streams[key]++
	rs.mu.Unlock()
	defer func() {
		rs.mu.Lock()
		defer rs.mu.Unlock()
		rs.streams[key]--
		if receiver {
			if p := rs.players[se.DeviceID]; p != nil && p.Connection == connection {
				p.attached = false
			}
		}
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-store")
	rc := http.NewResponseController(w)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !a.now().Before(se.Expires) {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-a.ctx.Done():
			return
		case <-se.ctx.Done():
			return
		default:
		}
		rs.mu.Lock()
		if se.DeviceID != "" {
			var d deviceSession
			if a.get("device", se.DeviceID, owner, &d) != nil || d.Revoked || !a.deviceLibraryValid(d) {
				rs.mu.Unlock()
				return
			}
		}
		a.remoteCleanup()
		var payload any
		if receiver {
			p := rs.players[se.DeviceID]
			if p == nil || p.Connection != connection {
				rs.mu.Unlock()
				return
			}
			commands := []any{}
			for _, c := range rs.commands {
				if c.Player == p.ID && c.Connection == connection && (c.Status == "queued" || c.Status == "delivered") && a.now().Before(c.Expires) {
					if c.Status == "queued" {
						c.Status = "delivered"
						if a.remoteSaveCommand(c, owner) != nil {
							rs.mu.Unlock()
							return
						}
					}
					commands = append(commands, map[string]any{"command": c, "remainingMs": c.Expires.Sub(a.now()).Milliseconds()})
				}
			}
			p.Online = a.remoteOnline(p)
			payload = map[string]any{"player": p, "commands": commands, "serverTime": a.now()}
		} else {
			payload = a.remotePlayers(owner, library)
		}
		rs.version++
		version := rs.version
		raw, err := json.Marshal(payload)
		rs.mu.Unlock()
		if err != nil {
			return
		}
		_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err = fmt.Fprintf(w, "id: %s:%d\nevent: snapshot\ndata: %s\n\n", rs.epoch, version, raw); err != nil {
			return
		}
		if rc.Flush() != nil {
			return
		}
		select {
		case <-revoked:
			return
		case <-ticker.C:
		case <-r.Context().Done():
			return
		case <-a.ctx.Done():
			return
		case <-se.ctx.Done():
			return
		}
	}
}
