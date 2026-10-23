package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"routengine/internal/telemetry"
)

// cmdStats is `routengine stats on|off|status|off-if-new`: the answer about usage statistics, from a
// terminal or from the installer, whose tasks page asks the same question as the window does.
func cmdStats(args []string) {
	if err := statsCommand(telemetry.DefaultPath(), args, os.Stdout); err != nil {
		die(err)
	}
}

func statsCommand(path string, args []string, out io.Writer) error {
	st := telemetry.Open(path)
	if len(args) != 1 {
		return errors.New("usage: routengine stats on|off|status")
	}
	switch args[0] {
	case "on":
		if err := st.Set(true); err != nil {
			return err
		}
	case "off":
		if err := st.Set(false); err != nil {
			return err
		}
	case "off-if-new": // the installer, for a box left unticked: no, unless the user already answered
		if !st.Asked() {
			if err := st.Set(false); err != nil {
				return err
			}
		}
	case "status":
	default:
		return errors.New("usage: routengine stats on|off|status")
	}
	switch {
	case st.Enabled():
		fmt.Fprintln(out, "usage statistics are ON: an anonymous report goes out about once a day (see the license agreement, section 6.2).")
	case st.Asked():
		fmt.Fprintln(out, "usage statistics are OFF: nothing is sent.")
	default:
		fmt.Fprintln(out, "usage statistics: not asked yet. Nothing is sent until you agree.")
	}
	return nil
}
