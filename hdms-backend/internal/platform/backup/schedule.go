package backup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
)

// ErrInvalidSchedule reports a schedule the console must not save.
var ErrInvalidSchedule = errors.New("backup: invalid schedule")

var timeLocalPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// ScheduleConfig is when the scheduled backup runs, in the worker's local
// time. IntervalMinutes, TimeLocal and Weekday are kept for every mode so the
// console can switch modes without losing the other fields.
type ScheduleConfig struct {
	Enabled         bool
	Mode            string // "interval" | "daily" | "weekly"
	IntervalMinutes int
	TimeLocal       string // "HH:MM"
	Weekday         int    // 0 = Sunday … 6
	UpdatedAt       time.Time
	UpdatedBy       string
}

func (c ScheduleConfig) Validate() error {
	switch c.Mode {
	case "interval":
		if c.IntervalMinutes < 15 || c.IntervalMinutes > 720 {
			return fmt.Errorf("%w: intervalMinutes must be between 15 and 720", ErrInvalidSchedule)
		}
	case "daily", "weekly":
		if !timeLocalPattern.MatchString(c.TimeLocal) {
			return fmt.Errorf("%w: timeLocal must be HH:MM", ErrInvalidSchedule)
		}
		if c.Mode == "weekly" && (c.Weekday < 0 || c.Weekday > 6) {
			return fmt.Errorf("%w: weekday must be between 0 and 6", ErrInvalidSchedule)
		}
	default:
		return fmt.Errorf("%w: mode must be interval, daily or weekly", ErrInvalidSchedule)
	}
	return nil
}

func (c ScheduleConfig) hourMinute() (int, int) {
	h, _ := strconv.Atoi(c.TimeLocal[:2])
	m, _ := strconv.Atoi(c.TimeLocal[3:])
	return h, m
}

// never is the schedule of a disabled backup: its latest instant is the zero
// time, which no job start is before, so it is never due.
type never struct{}

func (never) Latest(time.Time) time.Time { return time.Time{} }

// JobSchedule converts the configuration into the worker's schedule type.
// Callers validate first; an invalid configuration is treated as disabled.
func (c ScheduleConfig) JobSchedule(loc *time.Location) jobs.Schedule {
	if !c.Enabled || c.Validate() != nil {
		return never{}
	}
	switch c.Mode {
	case "interval":
		return jobs.Every(time.Duration(c.IntervalMinutes) * time.Minute)
	case "weekly":
		h, m := c.hourMinute()
		return jobs.WeeklyAt{Weekday: time.Weekday(c.Weekday), Hour: h, Minute: m, Loc: loc}
	default:
		h, m := c.hourMinute()
		return jobs.DailyAt{Hour: h, Minute: m, Loc: loc}
	}
}

// NextAfter is the first scheduled instant after now, for the console's
// "Next backup" line. It reports false when the schedule is disabled.
func (c ScheduleConfig) NextAfter(now time.Time, loc *time.Location) (time.Time, bool) {
	if !c.Enabled || c.Validate() != nil {
		return time.Time{}, false
	}
	latest := c.JobSchedule(loc).Latest(now)
	switch c.Mode {
	case "interval":
		return latest.Add(time.Duration(c.IntervalMinutes) * time.Minute), true
	case "weekly":
		l := latest.In(loc)
		return time.Date(l.Year(), l.Month(), l.Day()+7, l.Hour(), l.Minute(), 0, 0, loc), true
	default:
		l := latest.In(loc)
		return time.Date(l.Year(), l.Month(), l.Day()+1, l.Hour(), l.Minute(), 0, 0, loc), true
	}
}

const scheduleColumns = `enabled, mode, interval_minutes, time_local, weekday, updated_at, updated_by`

func scanSchedule(row interface{ Scan(...any) error }) (ScheduleConfig, error) {
	var c ScheduleConfig
	var weekday int16
	var interval int32
	if err := row.Scan(&c.Enabled, &c.Mode, &interval, &c.TimeLocal, &weekday, &c.UpdatedAt, &c.UpdatedBy); err != nil {
		return ScheduleConfig{}, err
	}
	c.IntervalMinutes = int(interval)
	c.Weekday = int(weekday)
	return c, nil
}

func GetSchedule(ctx context.Context, q db.DBTX) (ScheduleConfig, error) {
	c, err := scanSchedule(q.QueryRow(ctx, `SELECT `+scheduleColumns+` FROM backup_schedule WHERE id = 1`))
	if err != nil {
		return ScheduleConfig{}, fmt.Errorf("backup: get schedule: %w", err)
	}
	return c, nil
}

func SaveSchedule(ctx context.Context, q db.DBTX, c ScheduleConfig, actor string) (ScheduleConfig, error) {
	if err := c.Validate(); err != nil {
		return ScheduleConfig{}, err
	}
	saved, err := scanSchedule(q.QueryRow(ctx, `
		UPDATE backup_schedule
		SET enabled = $1, mode = $2, interval_minutes = $3, time_local = $4, weekday = $5,
		    updated_at = now(), updated_by = $6
		WHERE id = 1
		RETURNING `+scheduleColumns,
		c.Enabled, c.Mode, c.IntervalMinutes, c.TimeLocal, c.Weekday, actor))
	if err != nil {
		return ScheduleConfig{}, fmt.Errorf("backup: save schedule: %w", err)
	}
	return saved, nil
}
