package server

import (
	"encoding/json"
	"sync"
	"time"
)

type remoteEntry struct {
	EntryID  string  `json:"entryId"`
	TrackID  string  `json:"trackId"`
	Title    string  `json:"title,omitempty"`
	Artist   string  `json:"artist,omitempty"`
	Album    string  `json:"album,omitempty"`
	Duration float64 `json:"duration,omitempty"`
	CoverArt string  `json:"coverArt,omitempty"`
}
type remotePlayback struct {
	Kind     string          `json:"kind"`
	Queue    []remoteEntry   `json:"queue"`
	Current  string          `json:"currentEntryId,omitempty"`
	Station  string          `json:"stationId,omitempty"`
	Position int64           `json:"positionMs"`
	Play     bool            `json:"playWhenReady"`
	Held     *remotePlayback `json:"heldLibrary,omitempty"`
}
type remoteState struct {
	Metadata  *radioMetadata  `json:"radioMetadata,omitempty"`
	Playback  *remotePlayback `json:"playback"`
	Transport string          `json:"transport"`
	Position  int64           `json:"positionMs"`
	Duration  int64           `json:"durationMs"`
	Volume    float64         `json:"volume"`
	Revision  int64           `json:"receiverRevision"`
	Applied   string          `json:"lastAppliedCommandId"`
	Error     string          `json:"error"`
}
type remotePlayer struct {
	ID              string      `json:"id"`
	Owner           string      `json:"-"`
	Library         string      `json:"libraryId"`
	Name            string      `json:"name"`
	Kinds           []string    `json:"sourceKinds"`
	Commands        []string    `json:"commands"`
	Connection      string      `json:"connectionId"`
	Epoch           string      `json:"serverEpoch"`
	Revision        int64       `json:"revision"`
	Sequence        int64       `json:"reportSequence"`
	CommandSequence int64       `json:"commandSequence"`
	State           remoteState `json:"state"`
	Seen            time.Time   `json:"lastReportAt"`
	Online          bool        `json:"online"`
	attached        bool
	reportHash      string
}
type remoteCommand struct {
	ID         string          `json:"id"`
	Player     string          `json:"playerId"`
	Connection string          `json:"connectionId"`
	Revision   int64           `json:"receiverRevision"`
	Sequence   int64           `json:"commandSequence"`
	Type       string          `json:"type"`
	Playback   *remotePlayback `json:"playback,omitempty"`
	Position   int64           `json:"positionMs"`
	Volume     float64         `json:"volume"`
	Status     string          `json:"status"`
	Expires    time.Time       `json:"expiresAt"`
	Created    time.Time       `json:"createdAt"`
	Hash       string          `json:"-"`
	AckHash    string          `json:"ackHash,omitempty"`
}
type remoteStoredCommand struct {
	Command remoteCommand `json:"command"`
	Hash    string        `json:"hash"`
}
type remoteStore struct {
	lastCleanup  time.Time
	enrollmentMu sync.Mutex
	revoked      map[string]chan struct{}
	mu           sync.Mutex
	players      map[string]*remotePlayer
	commands     map[string]*remoteCommand
	streams      map[string]int
	epoch        string
	version      int64
	rates        map[string]remoteRate
}
type remoteRate struct {
	At    time.Time
	Count int
}

var remoteCommands = []string{"load", "replaceQueue", "play", "pause", "stop", "next", "previous", "seek", "setVolume"}

func remoteCapability() any {
	return map[string]any{"enabled": true, "revision": "2026-11-01.1", "sourceKinds": []string{"library", "radioLive"}, "commands": remoteCommands, "maxQueueEntries": 1000, "heartbeatMs": 10000, "offlineMs": 30000, "commandLifetimeMs": 15000, "maxReceivers": 16}
}
func remoteContains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
func remoteHash(v any) string { b, _ := json.Marshal(v); return digest(string(b)) }
func (a *apiV1) initRemote() error {
	a.remote = &remoteStore{revoked: map[string]chan struct{}{}, players: map[string]*remotePlayer{}, commands: map[string]*remoteCommand{}, streams: map[string]int{}, epoch: randomID(), rates: map[string]remoteRate{}}
	rows, err := a.list("remotePlayer", "*")
	if err != nil {
		return err
	}
	for _, raw := range rows {
		var v struct {
			Player remotePlayer
			Owner  string
		}
		if json.Unmarshal(raw, &v) != nil {
			continue
		}
		p := v.Player
		p.Owner = v.Owner
		p.Connection = ""
		p.Epoch = a.remote.epoch
		p.Online = false
		a.remote.players[p.ID] = &p
	}
	rows, err = a.list("remoteCommand", "*")
	if err != nil {
		return err
	}
	for _, raw := range rows {
		var v remoteStoredCommand
		if json.Unmarshal(raw, &v) != nil {
			continue
		}
		c := v.Command
		c.Hash = v.Hash
		if a.now().Sub(c.Created) > 24*time.Hour {
			_ = a.remove("remoteCommand", c.ID)
			continue
		}
		if c.Status == "queued" || c.Status == "delivered" {
			c.Status = "outcome_unknown"
		}
		a.remote.commands[c.ID] = &c
	}
	return nil
}
func (a *apiV1) remoteSavePlayer(p *remotePlayer) error {
	return a.put("remotePlayer", p.ID, p.Owner, struct {
		Player *remotePlayer
		Owner  string
	}{p, p.Owner})
}
func (a *apiV1) remoteSaveCommand(c *remoteCommand, owner string) error {
	return a.put("remoteCommand", c.ID, owner, remoteStoredCommand{*c, c.Hash})
}
func (a *apiV1) remoteOnline(p *remotePlayer) bool {
	return p.attached && a.now().Sub(p.Seen) < 30*time.Second && !p.Seen.IsZero()
}
func remoteValidPlayback(p *remotePlayback, depth int) bool {
	if p == nil || depth > 1 || p.Position < 0 || p.Position > 604800000 || len(p.Queue) > 1000 {
		return false
	}
	if p.Kind == "radioLive" {
		return depth == 0 && p.Position == 0 && len(p.Queue) == 0 && p.Station != "" && len(p.Station) <= 256 && (p.Held == nil || remoteValidPlayback(p.Held, 1))
	}
	if p.Kind != "library" || len(p.Queue) == 0 {
		return false
	}
	seen := map[string]bool{}
	found := false
	for _, e := range p.Queue {
		if e.EntryID == "" || e.TrackID == "" || len(e.EntryID) > 128 || len(e.TrackID) > 256 || seen[e.EntryID] || len(e.Title)+len(e.Artist)+len(e.Album)+len(e.CoverArt) > 2048 || e.Duration < 0 || e.Duration > 604800 {
			return false
		}
		seen[e.EntryID] = true
		found = found || e.EntryID == p.Current
	}
	return found && p.Held == nil
}
func remoteValidState(s remoteState) bool {
	return (s.Metadata == nil || len(s.Metadata.Title)+len(s.Metadata.Artist) <= 512) && (s.Playback == nil || s.Playback.Kind != "radioLive" || (s.Position == 0 && s.Duration == 0)) && remoteContains([]string{"idle", "buffering", "playing", "paused", "ended", "error"}, s.Transport) && s.Volume >= 0 && s.Volume <= 1 && s.Position >= 0 && s.Position <= 604800000 && s.Duration >= 0 && s.Duration <= 604800000 && s.Revision >= 0 && len(s.Error) <= 256 && (s.Playback == nil || remoteValidPlayback(s.Playback, 0))
}
