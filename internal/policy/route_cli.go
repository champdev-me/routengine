//go:build nogui

package policy

import "routengine/internal/engine"

// The command line has no Pro features: a rule routes, limits and caps, and nothing more.

func (r *Router) proRoute(*engine.Route, Rule) {}

func (r *Router) applyAdapterLimits(m *engine.AppMeter, _ Rule) { m.SetAdapterLimits(nil) }

func (r *Router) syncPro(*Set) {}
