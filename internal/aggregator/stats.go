// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package aggregator

import (
	"math"
	"sort"
)

func Median(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2, true
	}
	return sorted[mid], true
}

// FilterOutliers drops values that differ from the median by more than
// tolerancePct (interpreted as a fraction, e.g. 0.02 for 2%).
func FilterOutliers(values []float64, tolerancePct float64) []float64 {
	if len(values) <= 2 || tolerancePct <= 0 {
		return values
	}
	med, ok := Median(values)
	if !ok || med == 0 {
		return values
	}
	out := make([]float64, 0, len(values))
	for _, v := range values {
		diff := math.Abs(v-med) / med
		if diff <= tolerancePct {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return values
	}
	return out
}
