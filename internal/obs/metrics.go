// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package obs

import (
	"sort"
	"sync"
	"sync/atomic"
)

type gauge struct {
	mu sync.RWMutex
	v  float64
}

// Metrics is a tiny in-process metrics registry that exports a Prometheus
// text exposition format on demand. We avoid pulling in the official
// client_golang to keep the dependency surface minimal.
type Metrics struct {
	gauges   sync.Map // map[string]*gauge
	counters sync.Map // map[string]*uint64 counter
	labeled  sync.Map // map[string]*sync.Map{name -> map[label]*float64}
}

func New() *Metrics { return &Metrics{} }

func (m *Metrics) SetGauge(name string, value float64) {
	v, _ := m.gauges.LoadOrStore(name, &gauge{})
	v.(*gauge).set(value)
}

func (m *Metrics) GetGauge(name string) float64 {
	v, ok := m.gauges.Load(name)
	if !ok {
		return 0
	}
	return v.(*gauge).get()
}

func (m *Metrics) IncCounter(name string) {
	atomic.AddUint64(m.counter(name), 1)
}

func (m *Metrics) AddCounter(name string, delta uint64) {
	atomic.AddUint64(m.counter(name), delta)
}

func (m *Metrics) counter(name string) *uint64 {
	v, _ := m.counters.LoadOrStore(name, new(uint64))
	return v.(*uint64)
}

func (m *Metrics) SetLabeled(name, label string, value float64) {
	mv, _ := m.labeled.LoadOrStore(name, &sync.Map{})
	lm := mv.(*sync.Map)
	v, _ := lm.LoadOrStore(label, new(float64Mutex))
	mu := v.(*float64Mutex)
	mu.set(value)
}

func (m *Metrics) LabeledValue(name, label string) float64 {
	mv, ok := m.labeled.Load(name)
	if !ok {
		return 0
	}
	v, ok := mv.(*sync.Map).Load(label)
	if !ok {
		return 0
	}
	return v.(*float64Mutex).get()
}

// Text returns the Prometheus text exposition format for all metrics.
func (m *Metrics) Text() string {
	var out string
	gaugeNames := []string{}
	m.gauges.Range(func(k, _ any) bool {
		gaugeNames = append(gaugeNames, k.(string))
		return true
	})
	sort.Strings(gaugeNames)
	for _, name := range gaugeNames {
		v, _ := m.gauges.Load(name)
		out += "# TYPE " + name + " gauge\n"
		out += name + " " + formatFloat(v.(*gauge).get()) + "\n"
	}
	labeledNames := []string{}
	m.labeled.Range(func(k, _ any) bool {
		labeledNames = append(labeledNames, k.(string))
		return true
	})
	sort.Strings(labeledNames)
	for _, name := range labeledNames {
		v, _ := m.labeled.Load(name)
		labels := v.(*sync.Map)
		var rows []string
		labels.Range(func(k, vv any) bool {
			label := k.(string)
			val := vv.(*float64Mutex).get()
			rows = append(rows, name+"{"+label+"} "+formatFloat(val))
			return true
		})
		sort.Strings(rows)
		out += "# TYPE " + name + " gauge\n"
		for _, r := range rows {
			out += r + "\n"
		}
	}
	counterNames := []string{}
	m.counters.Range(func(k, _ any) bool {
		counterNames = append(counterNames, k.(string))
		return true
	})
	sort.Strings(counterNames)
	for _, name := range counterNames {
		v, _ := m.counters.Load(name)
		out += "# TYPE " + name + " counter\n"
		out += name + " " + formatUint(atomic.LoadUint64(v.(*uint64))) + "\n"
	}
	return out
}

func formatFloat(v float64) string {
	if v == 0 {
		return "0"
	}
	return strconvFloat(v)
}

func formatUint(v uint64) string {
	return strconvUint(v)
}

func (g *gauge) set(v float64) {
	g.mu.Lock()
	g.v = v
	g.mu.Unlock()
}

func (g *gauge) get() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.v
}

type float64Mutex struct {
	mu sync.RWMutex
	v  float64
}

func (f *float64Mutex) set(v float64) {
	f.mu.Lock()
	f.v = v
	f.mu.Unlock()
}

func (f *float64Mutex) get() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.v
}

