package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"routengine/internal/endpoints"
	"routengine/internal/license"
)

func cmdMachineCode() { fmt.Println(license.MachineCode()) }

// cmdLicense is `license add <key>`, `license status` and `license remove`: what the window's Settings does.
func cmdLicense(args []string) {
	path := license.DefaultPath()
	license.SetAppVersion(version)
	switch {
	case len(args) == 2 && args[0] == "add":
		l, err := license.Install(path, args[1])
		if err != nil {
			die(err)
		}
		fmt.Printf("Key accepted: %s, %s plan. Asking the licensing server...\n", l.Name, l.Plan)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := license.CheckOnce(ctx, &http.Client{Timeout: 20 * time.Second}, endpoints.Check, version); err != nil {
			fmt.Println("Could not reach the licensing server:", err)
			fmt.Println("Pro turns on after the first successful check; `routengine run` repeats it.")
			return
		}
		printLicense()
	case len(args) == 1 && args[0] == "status":
		printLicense()
	case len(args) == 1 && args[0] == "remove":
		if err := license.Remove(path); err != nil {
			die(err)
		}
		fmt.Println("License removed: the free edition.")
	default:
		fmt.Fprintln(os.Stderr, "usage: routengine license add <key> | status | remove")
		os.Exit(2)
	}
}

func printLicense() {
	l, err := license.Load(license.DefaultPath())
	fmt.Println("Machine code:", license.MachineCode())
	switch {
	case err != nil:
		fmt.Println("License: free edition,", err)
	case !license.Installed():
		fmt.Println("License: none (free edition). Send the machine code to buy a key.")
	default:
		fmt.Printf("License: %s, %s plan, covers %d PC(s)\n", l.Name, l.Plan, l.Seats())
		if !l.Expires.IsZero() {
			fmt.Println("Expires:", l.Expires.Format("2006-01-02"))
		}
		if why := license.Denial(); why != "" {
			fmt.Println("Not in force:", why)
		} else if left, ok := license.LeaseLeft(); ok {
			fmt.Printf("Verified by the licensing server; good for %s more without contact.\n", left.Round(time.Hour))
		}
	}
	fmt.Println("Key file:", license.DefaultPath())
}
