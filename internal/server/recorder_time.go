package server

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

func localBoundary(day time.Time, clock string, loc *time.Location) (time.Time, string, error) {
	c, e := time.Parse("15:04", clock)
	if e != nil {
		return time.Time{}, "", errors.New("use HH:MM local boundaries")
	}
	y, m, d := day.Date()
	wall := time.Date(y, m, d, c.Hour(), c.Minute(), 0, 0, time.UTC)
	offsets := map[int]bool{}
	for _, hours := range []int{-36, -12, 0, 12, 36} {
		_, off := wall.Add(time.Duration(hours) * time.Hour).In(loc).Zone()
		offsets[off] = true
	}
	exact := []time.Time{}
	var shifted time.Time
	best := time.Duration(1<<63 - 1)
	for off := range offsets {
		utc := wall.Add(-time.Duration(off) * time.Second)
		local := utc.In(loc)
		lw := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, time.UTC)
		delta := lw.Sub(wall)
		if delta == 0 {
			exact = append(exact, utc)
		} else if delta > 0 && delta < best {
			shifted = utc
			best = delta
		}
	}
	sort.Slice(exact, func(i, j int) bool { return exact[i].Before(exact[j]) })
	if len(exact) > 0 {
		reason := ""
		if len(exact) > 1 {
			reason = "Ambiguous " + clock + " uses its first occurrence."
		}
		return exact[0], reason, nil
	}
	if !shifted.IsZero() {
		return shifted, "Nonexistent " + clock + " shifted forward by " + best.String() + ".", nil
	}
	return time.Time{}, "", errors.New("could not resolve local boundary")
}
func scheduleWindow(in scheduleInput, after time.Time) (time.Time, time.Time, []string, error) {
	if in.Rule.Kind == "once" || in.Rule.Kind == "" {
		if in.Starts.IsZero() || in.Ends.IsZero() || !in.Starts.Before(in.Ends) {
			return time.Time{}, time.Time{}, nil, errors.New("choose exact UTC start and end times")
		}
		return in.Starts.UTC(), in.Ends.UTC(), []string{}, nil
	}
	loc, e := time.LoadLocation(in.Zone)
	if e != nil {
		return time.Time{}, time.Time{}, nil, errors.New("choose a valid IANA time zone")
	}
	if in.Rule.Kind != "daily" && in.Rule.Kind != "weekly" {
		return time.Time{}, time.Time{}, nil, errors.New("choose once, daily or weekly")
	}
	if in.Rule.LocalStart == in.Rule.LocalEnd {
		return time.Time{}, time.Time{}, nil, errors.New("start and end must differ")
	}
	if _, e = time.Parse("15:04", in.Rule.LocalStart); e != nil {
		return time.Time{}, time.Time{}, nil, e
	}
	if _, e = time.Parse("15:04", in.Rule.LocalEnd); e != nil {
		return time.Time{}, time.Time{}, nil, e
	}
	days := map[int]bool{}
	for _, d := range in.Rule.Weekdays {
		if d < 1 || d > 7 || days[d] {
			return time.Time{}, time.Time{}, nil, errors.New("weekdays must be unique ISO days 1–7")
		}
		days[d] = true
	}
	if in.Rule.Kind == "weekly" && len(days) == 0 {
		return time.Time{}, time.Time{}, nil, errors.New("choose at least one weekday")
	}
	local := after.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, loc)
	for n := 0; n < 370; n++ {
		candidate := day.AddDate(0, 0, n)
		if in.Rule.EndDate != nil {
			if _, e := time.Parse("2006-01-02", *in.Rule.EndDate); e != nil {
				return time.Time{}, time.Time{}, nil, e
			}
			if candidate.Format("2006-01-02") > *in.Rule.EndDate {
				return time.Time{}, time.Time{}, nil, errors.New("recurrence has ended")
			}
		}
		weekday := int(candidate.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		if in.Rule.Kind == "weekly" && !days[weekday] {
			continue
		}
		start, sc, e := localBoundary(candidate, in.Rule.LocalStart, loc)
		if e != nil {
			return time.Time{}, time.Time{}, nil, e
		}
		if !start.After(after) {
			continue
		}
		endDay := candidate
		if strings.Compare(in.Rule.LocalEnd, in.Rule.LocalStart) < 0 {
			endDay = candidate.AddDate(0, 0, 1)
		}
		end, ec, e := localBoundary(endDay, in.Rule.LocalEnd, loc)
		if e != nil {
			return time.Time{}, time.Time{}, nil, e
		}
		corrections := []string{}
		if sc != "" {
			corrections = append(corrections, sc)
		}
		if ec != "" {
			corrections = append(corrections, ec)
		}
		if !end.After(start) {
			return time.Time{}, time.Time{}, nil, errors.New("DST produces an empty window")
		}
		return start, end, corrections, nil
	}
	return time.Time{}, time.Time{}, nil, fmt.Errorf("no recurrence within one year")
}
