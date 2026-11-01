package server

import (
	"net/http"
	"time"
)

func (a *apiV1) remoteSubmit(w http.ResponseWriter, r *http.Request, se *session, p *remotePlayer) {
	key := r.Header.Get("Idempotency-Key")
	if !uuidKey.MatchString(key) {
		verror(w, 422, "validation_failed", "UUID required.")
		return
	}
	var in struct {
		Type     string          `json:"type"`
		Playback *remotePlayback `json:"playback,omitempty"`
		Position int64           `json:"positionMs"`
		Volume   float64         `json:"volume"`
	}
	if decode(r, &in) != nil || !remoteContains(p.Commands, in.Type) || in.Position < 0 || in.Position > 604800000 || in.Volume < 0 || in.Volume > 1 {
		verror(w, 422, "unsupported", "Invalid or unsupported control.")
		return
	}
	actor := se.DeviceID
	if actor == "" {
		cookie, err := r.Cookie("weazl_session")
		if err != nil {
			verror(w, 401, "unauthorized", "Sign in.")
			return
		}
		actor = digest(cookie.Value)
	}
	id := digest(p.Owner + p.Library + actor + p.ID + key)
	hash := remoteHash(struct {
		Body any
		Tag  string
	}{in, r.Header.Get("If-Match")})
	if c := a.remote.commands[id]; c != nil {
		if c.Hash != hash {
			verror(w, 409, "version_conflict", "Idempotency key already used.")
			return
		}
		dataOut(w, 202, c)
		return
	}
	if !precondition(w, r, p.Revision) {
		return
	}
	if !a.remoteOnline(p) {
		verror(w, 409, "player_offline", "Player is unreachable.")
		return
	}
	for _, c := range a.remote.commands {
		if c.Player == p.ID && (c.Status == "queued" || c.Status == "delivered" || c.Status == "outcome_unknown") && c.Connection == p.Connection {
			verror(w, 409, "player_busy", "Reconcile pending command before continuing.")
			return
		}
	}
	if in.Type == "load" || in.Type == "replaceQueue" {
		if !remoteValidPlayback(in.Playback, 0) || !remoteContains(p.Kinds, in.Playback.Kind) {
			verror(w, 422, "validation_failed", "Invalid playback source or queue.")
			return
		}
		if in.Playback.Kind == "radioLive" {
			collection, err := a.collection(se)
			found := false
			for _, station := range collection.Stations {
				if station.ID == in.Playback.Station {
					found = true
				}
			}
			if err != nil || !found {
				verror(w, 422, "validation_failed", "Save this station before transferring.")
				return
			}
		}
	}
	if remoteContains([]string{"play", "pause", "next", "previous", "seek"}, in.Type) && p.State.Playback == nil {
		verror(w, 422, "unsupported", "No source loaded.")
		return
	}
	if in.Type == "seek" && (p.State.Playback == nil || p.State.Playback.Kind != "library") {
		verror(w, 422, "unsupported", "This source cannot seek.")
		return
	}
	c := &remoteCommand{ID: id, Player: p.ID, Connection: p.Connection, Revision: p.State.Revision, Sequence: p.CommandSequence + 1, Type: in.Type, Playback: in.Playback, Position: in.Position, Volume: in.Volume, Status: "queued", Created: a.now(), Expires: a.now().Add(15 * time.Second), Hash: hash}
	if a.remoteSaveCommand(c, p.Owner) != nil {
		verror(w, 503, "storage_full", "Could not accept command.")
		return
	}
	p.CommandSequence = c.Sequence
	a.remote.commands[id] = c
	dataOut(w, 202, c)
}
