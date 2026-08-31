// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package obs

import "strconv"

func strconvFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func strconvUint(v uint64) string {
	return strconv.FormatUint(v, 10)
}
