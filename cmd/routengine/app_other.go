//go:build !windows && nogui

package main

import "errors"

// cmdApp: the command-line build has no window or tray; `run` is its engine.
func cmdApp(args []string) {
	die(errors.New("this build has no window; use `routengine run`, or install the RoutEngine app"))
}
