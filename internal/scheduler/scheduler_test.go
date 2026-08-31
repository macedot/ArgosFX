package scheduler

import (
	"testing"
	"time"
)

func TestComputeInterval_ExplicitCron(t *testing.T) {
	got := ComputeSchedule(1000, "0 */2 * * *")
	if got.Kind != KindCron {
		t.Errorf("kind: got %v, want cron", got.Kind)
	}
	if got.Cron != "0 */2 * * *" {
		t.Errorf("expr: got %q", got.Cron)
	}
}

func TestComputeInterval_CallsPerDayAuto(t *testing.T) {
	cases := []struct {
		callsPerDay int
		wantMin     time.Duration
		wantMax     time.Duration
	}{
		{8640, 8 * time.Second, 10 * time.Second},
		{1000, 70 * time.Second, 90 * time.Second},
		{60, 20 * time.Minute, 25 * time.Minute},
	}
	for _, tc := range cases {
		got := ComputeSchedule(tc.callsPerDay, "")
		if got.Kind != KindEvery {
			t.Errorf("calls_per_day=%d: kind got %v want every", tc.callsPerDay, got.Kind)
		}
		if got.Every < tc.wantMin || got.Every > tc.wantMax {
			t.Errorf("calls_per_day=%d: %v out of [%v, %v]", tc.callsPerDay, got.Every, tc.wantMin, tc.wantMax)
		}
	}
}

func TestComputeInterval_NoLimit(t *testing.T) {
	got := ComputeSchedule(0, "")
	if got.Kind != KindEvery {
		t.Errorf("kind got %v", got.Kind)
	}
	if got.Every != 5*time.Minute {
		t.Errorf("default: got %v, want 5m", got.Every)
	}
}

func TestBudgetAllowed(t *testing.T) {
	if !BudgetAllowed(5, 10) {
		t.Error("expected allowed when 5 < 10")
	}
	if BudgetAllowed(10, 10) {
		t.Error("expected not allowed when at limit")
	}
	if BudgetAllowed(11, 10) {
		t.Error("expected not allowed when over limit")
	}
	if !BudgetAllowed(5, 0) {
		t.Error("unlimited should allow")
	}
}
