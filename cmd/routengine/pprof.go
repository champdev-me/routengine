package main

import (
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/ on the default mux

	"routengine/internal/logx"
)

// startPprof serves Go's profiler for diagnosing CPU use, e.g.
//
//	go tool pprof -top http://127.0.0.1:6060/debug/pprof/profile?seconds=20
//
// Loopback only: profiles reveal internals and the endpoint has no authentication.
func startPprof(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("-pprof %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("-pprof must be a loopback address like 127.0.0.1:6060, got %q", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go http.Serve(ln, nil)
	logx.Printf("pprof on http://%s/debug/pprof/\n", addr)
	return nil
}
