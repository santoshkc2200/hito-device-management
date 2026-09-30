package jobs

import "time"

// Schedule reports the most recent instant, at or before now, when a job was
// meant to run. The worker runs a job when that instant is newer than the
// job's last start, so a missed window catches up once — and several missed
// windows still catch up only once, because only the latest is compared.
type Schedule interface {
	Latest(now time.Time) time.Time
}

// Every schedules a job on multiples of the duration since the Unix epoch.
// For the five-minute and hourly periods used here those are the wall-clock
// boundaries (:00, :05 …) in any whole-hour time zone.
type Every time.Duration

func (e Every) Latest(now time.Time) time.Time {
	return now.Truncate(time.Duration(e))
}

// DailyAt schedules a job once a day at a local wall-clock time.
type DailyAt struct {
	Hour, Minute int
	Loc          *time.Location
}

func (d DailyAt) Latest(now time.Time) time.Time {
	n := now.In(d.Loc)
	at := time.Date(n.Year(), n.Month(), n.Day(), d.Hour, d.Minute, 0, 0, d.Loc)
	if at.After(n) {
		at = time.Date(n.Year(), n.Month(), n.Day()-1, d.Hour, d.Minute, 0, 0, d.Loc)
	}
	return at
}

// WeeklyAt schedules a job once a week on a weekday at a local wall-clock time.
type WeeklyAt struct {
	Weekday      time.Weekday
	Hour, Minute int
	Loc          *time.Location
}

func (w WeeklyAt) Latest(now time.Time) time.Time {
	n := now.In(w.Loc)
	back := (int(n.Weekday()) - int(w.Weekday) + 7) % 7
	at := time.Date(n.Year(), n.Month(), n.Day()-back, w.Hour, w.Minute, 0, 0, w.Loc)
	if at.After(n) {
		at = time.Date(n.Year(), n.Month(), n.Day()-back-7, w.Hour, w.Minute, 0, 0, w.Loc)
	}
	return at
}

// Due reports whether a job should run now, given when it last started (the
// zero time if it never has).
func Due(s Schedule, now, lastStarted time.Time) bool {
	return lastStarted.Before(s.Latest(now))
}
