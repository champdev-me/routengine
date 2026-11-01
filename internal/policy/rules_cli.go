//go:build nogui

package policy

import "routengine/internal/engine"

// The command line has none of the window apps' Pro fields (its own DNS servers, the fastest adapter, speed caps per
// adapter). A rules file that has them, say one copied from the window app, still loads: they are kept and never
// applied, the way the free window app treats them.

func checkPro(Rule) error { return nil }

func checkFileAdapterLimits(map[string]engine.Speed) (map[string]engine.Speed, error) {
	return nil, nil
}
