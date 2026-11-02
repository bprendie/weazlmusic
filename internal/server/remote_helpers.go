package server

import (
	"encoding/json"
	"sort"
	"time"
)

func remoteDiscreteHash(s remoteState) string {
	s.Position = 0
	s.Duration = 0
	s.Applied = ""
	if s.Playback != nil {
		p := *s.Playback
		p.Position = 0
		s.Playback = &p
	}
	return remoteHash(s)
}
func remoteAppliedMatches(c *remoteCommand, s remoteState) bool {
	switch c.Type {
	case "load", "replaceQueue":
		if s.Playback == nil || c.Playback == nil {
			return false
		}
		playback := *s.Playback
		playback.Position = c.Playback.Position
		expected := "paused"
		if c.Playback.Play {
			expected = "playing"
		}
		return remoteHash(playback) == remoteHash(c.Playback) && s.Transport == expected
	case "play":
		return s.Transport == "playing"
	case "pause":
		return s.Transport == "paused"
	case "stop":
		return s.Transport == "idle" || s.Transport == "paused"
	case "setVolume":
		return s.Volume == c.Volume
	}
	return s.Transport != "error" && s.Transport != "buffering"
}
func (a *apiV1) remoteDeviceValid(p *remotePlayer) bool {
	var d deviceSession
	return a.get("device", p.ID, p.Owner, &d) == nil && !d.Revoked && d.Library == p.Library && a.deviceLibraryValid(d)
}
func (a *apiV1) remotePlayers(owner, library string) []*remotePlayer {
	out := []*remotePlayer{}
	for _, p := range a.remote.players {
		if p.Owner == owner && p.Library == library && a.remoteDeviceValid(p) {
			p.Online = a.remoteOnline(p)
			if p.Online {
				out = append(out, p)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out
}
func (a *apiV1) remoteCleanup() {
	if a.now().Sub(a.remote.lastCleanup) >= time.Minute {
		a.remote.lastCleanup = a.now()
		// Device refresh activity survives restarts; reports cover live receivers.
		// Expire only receiver registrations, preserving sign-in credentials.
		for id, p := range a.remote.players {
			var d deviceSession
			err := a.get("device", id, p.Owner, &d)
			lastActive := p.Seen
			if d.LastSeen.After(lastActive) {
				lastActive = d.LastSeen
			}
			if !a.remoteOnline(p) && (err != nil || d.Revoked || a.now().Sub(lastActive) >= 7*24*time.Hour) {
				if a.remove("remotePlayer", id) == nil {
					delete(a.remote.players, id)
				}
			}
		}
		rows, err := a.list("remoteEnrollment", "*")
		if err == nil {
			for _, raw := range rows {
				var old remoteEnrollment
				if json.Unmarshal(raw, &old) == nil && !a.now().Before(old.Expires) {
					if old.ID != "" {
						_ = a.remove("remoteEnrollment", old.ID)
					}
				}
			}
		}
	}

	for id, c := range a.remote.commands {
		if a.now().Sub(c.Created) > 24*time.Hour {
			if _, err := a.db.Exec("DELETE FROM records WHERE kind='remoteCommand' AND id=?", id); err == nil {
				delete(a.remote.commands, id)
			}
		} else if (c.Status == "queued" || c.Status == "delivered") && !a.now().Before(c.Expires) {
			c.Status = "outcome_unknown"
			p := a.remote.players[c.Player]
			if p != nil {
				_ = a.remoteSaveCommand(c, p.Owner)
			}
		}
	}
	for key, v := range a.remote.rates {
		if a.now().Sub(v.At) > time.Minute {
			delete(a.remote.rates, key)
		}
	}
}
