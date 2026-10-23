// Package logx is RoutEngine's console/log-file output, shared by every package.
package logx

import (
	"fmt"
	"io"
	"os"
)

var out io.Writer = os.Stdout

// SetOutput sends all further output to w (the -log file).
func SetOutput(w io.Writer) { out = w }

func Printf(format string, a ...any) { fmt.Fprintf(out, format, a...) }
