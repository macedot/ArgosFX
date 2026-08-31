// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package obs

import (
	"strings"
	"testing"
)

func TestGaugeAndCounter(t *testing.T) {
	m := New()
	m.SetGauge("foo", 1.5)
	m.SetGauge("foo", 2.5)
	m.IncCounter("bar")
	m.IncCounter("bar")
	m.AddCounter("bar", 3)

	text := m.Text()
	if !strings.Contains(text, "foo 2.5") {
		t.Errorf("missing gauge value: %q", text)
	}
	if !strings.Contains(text, "bar 5") {
		t.Errorf("missing counter value: %q", text)
	}
	if !strings.Contains(text, "# TYPE foo gauge") {
		t.Errorf("missing TYPE foo")
	}
	if !strings.Contains(text, "# TYPE bar counter") {
		t.Errorf("missing TYPE bar")
	}
}

func TestLabeledGauge(t *testing.T) {
	m := New()
	m.SetLabeled("qux", `provider="frankfurter"`, 7.0)
	m.SetLabeled("qux", `provider="yahoo"`, 8.0)

	text := m.Text()
	if !strings.Contains(text, `qux{provider="frankfurter"} 7`) {
		t.Errorf("missing labeled frankfurter: %q", text)
	}
	if !strings.Contains(text, `qux{provider="yahoo"} 8`) {
		t.Errorf("missing labeled yahoo: %q", text)
	}
}

func TestFormatZero(t *testing.T) {
	m := New()
	m.SetGauge("zero", 0)
	text := m.Text()
	if !strings.Contains(text, "zero 0") {
		t.Errorf("expected zero gauge to render: %q", text)
	}
}
