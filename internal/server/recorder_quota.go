package server

import (
	"encoding/json"
	"errors"
	"sort"
	"syscall"
	"time"
)

type recordingWindow struct {
	Start, End time.Time
	Count      int
}
type storageStatus struct {
	AccountUsed      int64 `json:"accountUsedBytes"`
	AccountBudget    int64 `json:"accountBudgetBytes"`
	AccountAvailable int64 `json:"accountAvailableBytes"`
	Used             int64 `json:"usedBytes"`
	Budget           int64 `json:"budgetBytes"`
	Available        int64 `json:"availableBytes"`
	Reserve          int64 `json:"reserveBytes"`
	Reserved         int64 `json:"reservedBytes"`
	Retention        any   `json:"retention"`
}

func (a *apiV1) storage() (storageStatus, error) {
	budget := a.s.cfg.CaptureBudgetBytes
	if budget <= 0 {
		budget = 20 << 30
	}
	reserve := a.s.cfg.ReserveBytes
	if reserve <= 0 {
		reserve = 1 << 30
	}
	v := storageStatus{Budget: budget, Reserve: reserve, Retention: map[string]any{"enabled": a.s.cfg.RetentionDays > 0, "days": a.s.cfg.RetentionDays}}
	rows, e := a.list("job", "*")
	if e != nil {
		return v, e
	}
	for _, row := range rows {
		var j recordingJob
		if json.Unmarshal(row, &j) != nil {
			return v, errors.New("invalid recording record")
		}
		if j.Tombstone {
			continue
		}
		v.Used += j.Bytes
		if !finalState(j.State) {
			v.Reserved += max(int64(0), j.Reserved-j.Bytes)
		}
	}
	var fs syscall.Statfs_t
	if e = syscall.Statfs(a.s.cfg.DataDir, &fs); e != nil {
		return v, e
	}
	free := int64(fs.Bavail)*int64(fs.Bsize) - reserve
	v.Available = max(int64(0), min(budget-v.Used-v.Reserved, free-v.Reserved))
	return v, nil
}
func estimateBytes(duration time.Duration, stations int) int64 {
	return int64(duration.Seconds()) * 40000 * int64(stations)
}
func overlapValid(windows []recordingWindow) bool {
	type edge struct {
		At time.Time
		N  int
	}
	edges := []edge{}
	for _, w := range windows {
		edges = append(edges, edge{w.Start, w.Count}, edge{w.End, -w.Count})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].At.Equal(edges[j].At) {
			return edges[i].N < edges[j].N
		}
		return edges[i].At.Before(edges[j].At)
	})
	count := 0
	for _, e := range edges {
		count += e.N
		if count > 6 {
			return false
		}
	}
	return true
}
func recurringWindows(in scheduleInput, after time.Time, count int) []recordingWindow {
	out := []recordingWindow{}
	if in.Rule.Kind == "once" || in.Rule.Kind == "" {
		return []recordingWindow{{in.Starts, in.Ends, count}}
	}
	cursor := after
	for i := 0; i < 400; i++ {
		s, e, _, err := scheduleWindow(in, cursor)
		if err != nil || s.After(after.AddDate(1, 0, 1)) {
			break
		}
		if e.Sub(s) <= 12*time.Hour {
			out = append(out, recordingWindow{s, e, count})
		}
		cursor = s.Add(time.Second)
	}
	return out
}
func (a *apiV1) capacity(in scheduleInput, count int, exclude string) bool {
	windows := recurringWindows(in, a.now(), count)
	schedules, e := a.list("schedule", "*")
	if e != nil {
		return false
	}
	scheduled := map[string]bool{}
	for _, row := range schedules {
		var s recordingSchedule
		if json.Unmarshal(row, &s) != nil {
			return false
		}
		if s.State != "active" || s.ID == exclude {
			continue
		}
		scheduled[s.ID] = true
		windows = append(windows, recurringWindows(s.scheduleInput, a.now(), len(s.Snapshots))...)
	}
	jobs, e := a.list("job", "*")
	if e != nil {
		return false
	}
	for _, row := range jobs {
		var j recordingJob
		if json.Unmarshal(row, &j) != nil {
			return false
		}
		if j.ScheduleID == exclude && j.State == "scheduled" {
			continue
		}
		if !finalState(j.State) && !scheduled[j.ScheduleID] {
			windows = append(windows, recordingWindow{j.Starts, j.Ends, j.StationCount})
		}
	}
	return overlapValid(windows)
}

func (a *apiV1) accountStorage(user string) (int64, int64, error) {
	rows, e := a.list("job", accountID(user))
	var used, reserved int64
	for _, row := range rows {
		var j recordingJob
		if json.Unmarshal(row, &j) != nil {
			return 0, 0, errors.New("invalid recording record")
		}
		if j.Tombstone {
			continue
		}
		used += j.Bytes
		if !finalState(j.State) {
			reserved += max(int64(0), j.Reserved-j.Bytes)
		}
	}
	return used, reserved, e
}

func (a *apiV1) accountBudget() int64 {
	if a.s.cfg.AccountBudgetBytes > 0 {
		return a.s.cfg.AccountBudgetBytes
	}
	return 10 << 30
}
