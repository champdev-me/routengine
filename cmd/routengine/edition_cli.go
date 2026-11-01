//go:build nogui

package main

import (
	"routengine/internal/policy"
	"routengine/internal/telemetry"
	"routengine/internal/tun"
	usagelog "routengine/internal/usage"
)

// The command line: no license and nothing to buy. It has every free feature, on any number of apps (see
// internal/edition), and none of the window apps' Pro features or the commands that go with them.

const proUsage = ""

func proCommand(string, []string) bool { return false }

func startLicense() string { return "" }

func proFacts(*telemetry.Facts) {}

func watchAdapterLimits(*policy.Router, *tun.Tun) {}

// gatedUsage shows today's data, not the month's, as the free window app does.
func gatedUsage(r usagelog.Report) usagelog.Report { return todayOnly(r) }
