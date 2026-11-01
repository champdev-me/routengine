// Package edition says what the program may do. The command line is one edition, with no license and nothing to
// buy: every free feature, on any number of apps. The window apps hand the question to their license instead
// (it sets the source when it loads), and then the free edition's limits and Pro apply.
package edition

// Features is what may be used.
type Features struct {
	UnlimitedApps  bool // any number of apps, not just FreeApps
	SpeedLimit     bool // a speed limit on every app
	DataCap        bool // a data cap on every app
	SpeedLimitApps int  // without SpeedLimit: how many apps may still have one
	CapApps        int  // without DataCap: how many apps may still have one
}

// FreeApps is how many apps the free window app looks after.
const FreeApps = 3

// CommandLine is the command line's edition: the free features on every app.
var CommandLine = Features{UnlimitedApps: true, SpeedLimit: true, DataCap: true}

var source = func() Features { return CommandLine }

// Allowed is what may be used now.
func Allowed() Features { return source() }

// SetSource has Allowed ask f from now on. The window apps' license calls it once, before anything routes.
func SetSource(f func() Features) { source = f }

// ForSlots is f for one app that holds the given speed-limit and data-cap places (-1 for none): an app within the
// first SpeedLimitApps that use a limit may have its limit even where SpeedLimit is off, and the same for caps.
func (f Features) ForSlots(speed, cap int) Features {
	if speed >= 0 && speed < f.SpeedLimitApps {
		f.SpeedLimit = true
	}
	if cap >= 0 && cap < f.CapApps {
		f.DataCap = true
	}
	return f
}
