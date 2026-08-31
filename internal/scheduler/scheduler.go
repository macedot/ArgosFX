package scheduler

import (
	"context"
	"errors"
	"time"
)

type ScheduleKind int

const (
	KindEvery ScheduleKind = iota
	KindCron
)

type Schedule struct {
	Kind  ScheduleKind
	Cron  string        // valid when Kind == KindCron
	Every time.Duration // valid when Kind == KindEvery
}

// ComputeSchedule picks the polling cadence for a provider.
//   - If cronExpr is non-empty: returns it as a cron.
//   - Else if callsPerDay > 0: auto-spaces across 24h with 10% safety margin.
//   - Else: defaults to every 5 minutes.
func ComputeSchedule(callsPerDay int, cronExpr string) Schedule {
	if cronExpr != "" {
		return Schedule{Kind: KindCron, Cron: cronExpr}
	}
	if callsPerDay <= 0 {
		return Schedule{Kind: KindEvery, Every: 5 * time.Minute}
	}
	seconds := float64(86400) / float64(callsPerDay) * 0.9
	if seconds < 1 {
		seconds = 1
	}
	return Schedule{Kind: KindEvery, Every: time.Duration(seconds * float64(time.Second))}
}

func BudgetAllowed(usedToday, callsPerDay int) bool {
	if callsPerDay <= 0 {
		return true
	}
	return usedToday < callsPerDay
}

type JobFunc func(ctx context.Context) error

// Jitter returns a pseudo-random duration in [0, d).
func Jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(time.Now().UnixNano() % int64(d))
}

var ErrShuttingDown = errors.New("scheduler shutting down")

