// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package aggregator

import (
	"math"
	"testing"
)

func TestMedian_OddCount(t *testing.T) {
	got, ok := Median([]float64{3, 1, 2})
	if !ok || got != 2 {
		t.Errorf("got %v ok=%v", got, ok)
	}
}

func TestMedian_EvenCount(t *testing.T) {
	got, ok := Median([]float64{4, 1, 3, 2})
	if !ok || got != 2.5 {
		t.Errorf("got %v ok=%v", got, ok)
	}
}

func TestMedian_Empty(t *testing.T) {
	_, ok := Median(nil)
	if ok {
		t.Error("expected !ok for empty")
	}
}

func TestFilterOutliers_DropsOffenders(t *testing.T) {
	in := []float64{1.00, 1.01, 1.02, 1.50, 0.50}
	got := FilterOutliers(in, 0.05)
	if math.Abs(got[0]-1.00) > 1e-9 || math.Abs(got[1]-1.01) > 1e-9 || math.Abs(got[2]-1.02) > 1e-9 {
		t.Errorf("unexpected kept values: %v", got)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 kept, got %d", len(got))
	}
}

func TestFilterOutliers_AllKept(t *testing.T) {
	in := []float64{1.00, 1.01, 1.02}
	got := FilterOutliers(in, 0.05)
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
}

func TestFilterOutliers_ZeroMedianReturnsOriginal(t *testing.T) {
	in := []float64{0, 0, 0}
	got := FilterOutliers(in, 0.05)
	if len(got) != 3 {
		t.Errorf("expected original when median is 0, got %d", len(got))
	}
}

func TestFilterOutliers_SmallInputPassthrough(t *testing.T) {
	in := []float64{1.0, 100.0}
	got := FilterOutliers(in, 0.05)
	if len(got) != 2 {
		t.Errorf("expected 2 passthrough, got %d", len(got))
	}
}
