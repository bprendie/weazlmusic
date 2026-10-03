package server

import "time"

type recurrence struct {
	Kind       string  `json:"kind"`
	LocalStart string  `json:"localStart,omitempty"`
	LocalEnd   string  `json:"localEnd,omitempty"`
	Weekdays   []int   `json:"weekdays,omitempty"`
	EndDate    *string `json:"endDate"`
}
type scheduleInput struct {
	Name       string     `json:"name"`
	StationIDs []string   `json:"stationIds"`
	Starts     time.Time  `json:"startsAt"`
	Ends       time.Time  `json:"endsAt"`
	Zone       string     `json:"timeZone"`
	Rule       recurrence `json:"recurrence"`
}
type recordingSchedule struct {
	scheduleInput
	ID          string          `json:"id"`
	Version     int64           `json:"version"`
	State       string          `json:"state"`
	Owner       string          `json:"-"`
	Username    string          `json:"username"`
	Library     string          `json:"libraryId"`
	StateKey    string          `json:"stateKey"`
	NextStarts  time.Time       `json:"nextStartsAt"`
	NextEnds    time.Time       `json:"nextEndsAt"`
	Snapshots   []nativeStation `json:"stationSnapshots"`
	Corrections []string        `json:"timeCorrections"`
	Error       string          `json:"error,omitempty"`
}
type stationProgress struct {
	Station   nativeStation       `json:"station"`
	StationID string              `json:"stationId"`
	State     string              `json:"state"`
	Duration  int64               `json:"capturedDurationMs"`
	Bytes     int64               `json:"bytes"`
	Error     string              `json:"error,omitempty"`
	Segments  []recordingSegment  `json:"segments"`
	Metadata  []recordingMetadata `json:"metadata"`
}
type recordingJob struct {
	ID               string            `json:"id"`
	ScheduleID       string            `json:"scheduleId,omitempty"`
	Name             string            `json:"name"`
	State            string            `json:"state"`
	Username         string            `json:"username"`
	Library          string            `json:"libraryId"`
	Starts           time.Time         `json:"startsAt"`
	Ends             time.Time         `json:"endsAt"`
	ActualStarted    *time.Time        `json:"actualStartedAt"`
	Duration         int64             `json:"durationMs"`
	StationCount     int               `json:"stationCount"`
	Bytes            int64             `json:"capturedBytes"`
	Stations         []stationProgress `json:"stations"`
	StopReason       string            `json:"stopReason,omitempty"`
	ManifestRevision string            `json:"manifestRevision,omitempty"`
	Version          int64             `json:"version"`
	Deleted          bool              `json:"-"`
	Tombstone        bool              `json:"deleted"`
	Reserved         int64             `json:"reservedBytes"`
}
type recordingSegment struct {
	ID          string `json:"id"`
	AssetID     string `json:"assetId"`
	Start       int64  `json:"startMs"`
	Duration    int64  `json:"durationMs"`
	MediaStart  int64  `json:"mediaStartMs"`
	Bytes       int64  `json:"byteLength"`
	Hash        string `json:"sha256"`
	ContentType string `json:"contentType"`
	Codec       string `json:"codec"`
}
type recordingGap struct {
	Start    int64  `json:"startMs"`
	Duration int64  `json:"durationMs"`
	Reason   string `json:"reason"`
}
type recordingMetadata struct {
	At     int64  `json:"atMs"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}
type recordingTrack struct {
	StationID string              `json:"stationId"`
	Name      string              `json:"name"`
	Order     int                 `json:"order"`
	Segments  []recordingSegment  `json:"segments"`
	Gaps      []recordingGap      `json:"gaps"`
	Metadata  []recordingMetadata `json:"metadata"`
}
type recordingManifest struct {
	Schema   int              `json:"schemaVersion"`
	Session  string           `json:"sessionId"`
	Revision string           `json:"revision"`
	Name     string           `json:"name"`
	Starts   time.Time        `json:"startsAt"`
	Duration int64            `json:"durationMs"`
	State    string           `json:"state"`
	Bytes    int64            `json:"totalBytes"`
	Tracks   []recordingTrack `json:"tracks"`
}

func finalState(s string) bool {
	return s == "complete" || s == "partial" || s == "failed" || s == "missed" || s == "cancelled"
}
