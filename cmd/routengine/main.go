package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"routengine/internal/endpoints"
	"routengine/internal/engine"
	"routengine/internal/icon"
	"routengine/internal/license"
	"routengine/internal/logx"
	"routengine/internal/nics"
	"routengine/internal/platform"
	"routengine/internal/policy"
	"routengine/internal/state"
	"routengine/internal/telemetry"
	"routengine/internal/tun"
	"routengine/internal/update"
)

const usage = `RoutEngine: choose which network adapter each app uses.

  routengine app        the RoutEngine app: tray icon, window and routing in one program (Windows; the default)
  routengine tray       the same on Windows; on other systems the tray icon on its own
  routengine run        system-wide ON in a console window (asks for admin/root); Ctrl+C or "off" turns it OFF
  routengine on|off     start/stop routing in the running app (off also works for run)
  routengine quit       stop routing and quit the running app
  routengine status     show whether it is on, and per-adapter load
  routengine socks      plain SOCKS5 proxy on 127.0.0.1:1080 (no admin, per-app)
  routengine list       show detected adapters
  routengine autostart on|off|status   start the tray at sign-in (Windows: no UAC prompt)
  routengine license add <key>|status|remove   enter or check the Pro license key
  routengine machine-code   the code to send when buying a license for this PC
  routengine stats on|off|status   send anonymous usage statistics, or not (nothing is sent until you agree)
  routengine cleanup    remove leftover routes after a crash (admin/root)
  routengine install    sudo: macOS lets the tray start without a password; Linux installs the systemd service (uninstall removes it)
  routengine version

Flags for run/socks: -include "WiFi,Ethernet 3"  -weight "Name=3"  -auto-weight=false  -stats 5s
`

type weightFlags map[string]float64

func (w weightFlags) String() string { return "" }
func (w weightFlags) Set(v string) error {
	i := strings.LastIndex(v, "=")
	if i < 1 {
		return fmt.Errorf(`want "Name=3", got %q`, v)
	}
	n, err := strconv.ParseFloat(v[i+1:], 64)
	if err != nil || n <= 0 {
		return fmt.Errorf("weight must be > 0 in %q", v)
	}
	w[v[:i]] = n
	return nil
}

type common struct {
	include  string
	weights  weightFlags
	rescan   time.Duration
	stats    time.Duration
	auto     bool
	logFile  string
	stateDir string
	ipv6     string
	mtu      int
	pprof    string
	rules    string
}

func (c *common) register(fs *flag.FlagSet) {
	c.weights = weightFlags{}
	fs.StringVar(&c.include, "include", "", "comma-separated adapter names to use (default: all non-virtual)")
	fs.Var(c.weights, "weight", `fixed adapter weight, repeatable: -weight "Wi-Fi=3"`)
	fs.DurationVar(&c.rescan, "rescan", 3*time.Second, "adapter re-scan interval")
	fs.DurationVar(&c.stats, "stats", 5*time.Second, "stats print interval (0 = off)")
	fs.BoolVar(&c.auto, "auto-weight", true, "weight adapters by measured throughput (off when -weight is given)")
	fs.StringVar(&c.ipv6, "ipv6", "auto", "auto: IPv6 only if every adapter has it (else apps use IPv4 on all adapters); on: always; off: never")
	fs.IntVar(&c.mtu, "mtu", tun.DefaultMTU, "MTU of the virtual adapter (1280-65535); larger means fewer packets and less CPU")
	fs.StringVar(&c.rules, "rules", "", "per-app routing rules (JSON); default: the rules.json in your config folder, if present")
	fs.StringVar(&c.pprof, "pprof", "", "serve Go's profiler on this loopback address, e.g. 127.0.0.1:6060")
	fs.StringVar(&c.logFile, "log", "", "write output to this file instead of the console")
	fs.StringVar(&c.stateDir, "state-dir", state.DefaultDir(), "where status/off marker files live")
}

func (c *common) nicsDetailed() ([]nics.Nic, []nics.Skip, error) {
	var inc []string
	for _, s := range strings.Split(c.include, ",") {
		if s = strings.TrimSpace(s); s != "" {
			inc = append(inc, s)
		}
	}
	return nics.DiscoverNicsDetailed(inc, c.weights)
}

func (c *common) nics() ([]nics.Nic, error) {
	nics, _, err := c.nicsDetailed()
	return nics, err
}

// setup opens the log file (if any) and the state dir.
func (c *common) setup() error {
	// Without admin (`socks`) there may be no state dir to make, e.g. the root-owned one on macOS.
	if err := state.Prepare(c.stateDir); err != nil && platform.IsAdmin() {
		return err
	}
	if c.logFile != "" {
		f, err := os.OpenFile(c.logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		logx.SetOutput(f)
		platform.RedirectStd(f)
	}
	return nil
}

// boot discovers adapters and starts the background loops: re-scan, 1s throughput sampling
// (which also drives auto-weights and the status file), and periodic stats printing.
func (c *common) boot(mode func() string) (*engine.Scheduler, *engine.Balancer, error) {
	if c.mtu < 1280 || c.mtu > 65535 {
		return nil, nil, fmt.Errorf("-mtu must be between 1280 and 65535, got %d", c.mtu)
	}
	if c.pprof != "" {
		if err := startPprof(c.pprof); err != nil {
			return nil, nil, err
		}
	}
	nics, err := c.nics()
	if err != nil {
		return nil, nil, err
	}
	if len(nics) == 0 {
		return nil, nil, fmt.Errorf("no usable IPv4 adapters found (try: routengine list)")
	}
	sched := engine.NewScheduler(nics)
	sched.SetAuto(c.auto && len(c.weights) == 0)
	for _, n := range nics {
		logx.Printf("  + %s\n", describeNic(n))
	}
	if len(nics) == 1 {
		logx.Printf("  note: only one adapter found, nothing to aggregate yet\n")
	}

	bal := engine.NewBalancer(sched)
	v6, ok := engine.ParseV6Mode(c.ipv6)
	if !ok {
		return nil, nil, fmt.Errorf(`-ipv6 must be auto, on or off, got %q`, c.ipv6)
	}
	bal.SetV6Mode(v6)

	since := time.Now()
	go sched.RunProbes(nil)
	go func() {
		for range time.Tick(c.rescan) {
			if ns, err := c.nics(); err == nil {
				sched.Update(ns)
			}
		}
	}()
	go func() {
		last := time.Now()
		for now := range time.Tick(time.Second) {
			sched.Tick(now.Sub(last))
			last = now
		}
	}()
	go func() { // the window reads this twice a second, so per-app speeds are sampled that often too
		for now := range time.Tick(engine.SampleEvery) {
			if mode != nil {
				if m := mode(); m != "" {
					state.Write(c.stateDir, state.Status{PID: os.Getpid(), Mode: m, V6: bal.V6State(), Since: since, Updated: now, NICs: sched.Snapshot(), Apps: engine.Apps.Snapshot(), Usage: gatedUsage(engine.Apps.UsageReport())})
				}
			}
		}
	}()
	if c.stats > 0 {
		go func() {
			for range time.Tick(c.stats) {
				logx.Printf("--- %s\n%s", time.Now().Format("15:04:05"), formatNICs(sched.Snapshot()))
			}
		}()
	}
	return sched, bal, nil
}

func describeNic(n nics.Nic) string {
	s := n.Name
	if n.IP != nil {
		s += "  v4 " + n.IP.String()
	}
	if n.IP6 != nil {
		s += "  v6 " + n.IP6.String()
	}
	if n.Kind != nics.KindInternet {
		s += "  [" + n.Kind.String() + ": used only when a rule names it]"
	}
	s += "  (" + n.Conn.String() + ")"
	return s
}

func formatNICs(snap []engine.SlotStats) string {
	var sb strings.Builder
	for _, s := range snap {
		gone := ""
		if !s.Present {
			gone = " (gone)"
		}
		// "link" is the adapter's total traffic by any app (Windows' counters); "via RoutEngine" is the
		// part RoutEngine relayed. Apps that bind to adapters directly (qBittorrent) only show in "link".
		fmt.Fprintf(&sb, "%-14s v4 %-11s v6 %-11s link down %6.1f up %5.1f Mbit/s  via RoutEngine %6.1f  active %3d  total %5d  w %.2f/%.2f  failed %d%s\n",
			s.Name, s.State, s.State6, s.LinkIn*8/1e6, s.LinkOut*8/1e6, (s.RateIn+s.RateOut)*8/1e6, s.Active, s.Total, s.Weight, s.Weight6, s.Failed, gone)
	}
	return sb.String()
}

// Set at build time: -X main.version=... and, for the windowless tray build, -X main.defaultCmd=tray.
var (
	version    = "dev"
	defaultCmd = ""
)

func main() {
	var cmd string
	var args []string
	switch {
	case len(os.Args) >= 2:
		cmd, args = os.Args[1], os.Args[2:]
	case defaultCmd != "":
		cmd = defaultCmd
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if cmd != "app" && cmd != "tray" {
		platform.AttachParentConsole() // the windowless build prints into the terminal it was started from
	}
	switch cmd {
	case "version":
		fmt.Println("RoutEngine", version)
	case "install", "uninstall":
		if err := platform.Install(cmd == "uninstall"); err != nil {
			die(err)
		}
	case "autostart":
		if len(args) != 1 || (args[0] != "on" && args[0] != "off" && args[0] != "status") {
			fmt.Fprintln(os.Stderr, "usage: routengine autostart on|off|status")
			os.Exit(2)
		}
		if err := platform.Autostart(args[0]); err != nil {
			die(err)
		}
	case "stats":
		cmdStats(args)
	case "license":
		cmdLicense(args)
	case "machine-code":
		cmdMachineCode()
	case "_icon": // build helper: writes the tray icon as an .ico
		if len(args) != 1 {
			os.Exit(2)
		}
		if err := os.WriteFile(args[0], icon.ICO(true), 0o644); err != nil {
			die(err)
		}
	case "tray", "app":
		cmdApp(args)
	case "run":
		cmdRun(args)
	case "on", "off", "quit":
		cmdControl(cmd, args)
	case "status":
		cmdStatus(args)
	case "socks":
		cmdSocks(args)
	case "list":
		cmdList(args)
	case "cleanup":
		cmdCleanup()
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	if !platform.HasConsole() {
		platform.ShowError(err.Error())
	}
	os.Exit(1)
}

func cmdList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	var c common
	c.register(fs)
	fs.Parse(args)
	nics, skipped, err := c.nicsDetailed()
	if err != nil {
		die(err)
	}
	if len(nics) == 0 {
		fmt.Println("No usable adapters found.")
	}
	for _, n := range nics {
		fmt.Printf("USE   %s  (index %d)\n", describeNic(n), n.Index)
	}
	for _, s := range skipped {
		fmt.Printf("skip  %s: %s\n", s.Name, s.Reason)
	}
}

func cmdSocks(args []string) {
	fs := flag.NewFlagSet("socks", flag.ExitOnError)
	var c common
	c.register(fs)
	listen := fs.String("listen", "127.0.0.1:1080", "SOCKS5 listen address")
	fs.Parse(args)
	if err := c.setup(); err != nil {
		die(err)
	}
	_, bal, err := c.boot(nil)
	if err != nil {
		die(err)
	}
	ln, err := net.Listen("tcp4", *listen)
	if err != nil {
		die(err)
	}
	logx.Printf("RoutEngine SOCKS5 listening on %s\n", *listen)
	die(engine.ServeSocks5(ln, bal))
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var c common
	c.register(fs)
	hidden := fs.Bool("hidden", false, "when elevating, hide the new console window")
	crashAfter := fs.Duration("crash-after", 0, "test only: exit abruptly (no cleanup) after this long")
	runFor := fs.Duration("for", 0, "turn off automatically after this long (0 = until stopped)")
	fs.Parse(args)

	if !platform.IsAdmin() {
		// Re-run elevated, handing over where to log and keep state.
		child := append([]string{"run"}, args...)
		child = append(child, "-state-dir", c.stateDir)
		state.Prepare(c.stateDir) // as this user, so `routengine off` can still drop markers in it
		if err := platform.RelaunchElevated(child, *hidden); err != nil {
			die(fmt.Errorf("could not start elevated: %w", err))
		}
		fmt.Println("Approve the UAC prompt. System-wide mode runs in the new window; `routengine off` turns it off.")
		return
	}
	if !platform.SingleInstance() {
		fmt.Println("RoutEngine is already running (check the system tray, or run `routengine off`).")
		return
	}
	if err := c.setup(); err != nil {
		die(err)
	}
	if err := platform.CheckDriver(); err != nil {
		logx.Printf("error: %v\n", err)
		os.Exit(1)
	}
	state.TakeMarker(c.stateDir, "off") // drop a stale marker

	sched, bal, err := c.boot(func() string { return "system-wide" })
	if err != nil {
		logx.Printf("error: %v\n", err)
		os.Exit(1)
	}
	t := tun.New(bal)
	t.MTU = c.mtu
	if err := c.attachRoutes(t); err != nil {
		logx.Printf("error: %v\n", err)
		os.Exit(1)
	}
	license.Load(license.DefaultPath()) // a saved key that was changed must block this too
	license.Logf = logx.Printf
	go license.RunChecks(context.Background(), version, nil)                       // Pro starts only after the licensing server says yes
	go update.Notify(context.Background(), endpoints.Update, version, logx.Printf) // says when a newer version exists; installs nothing
	// Anonymous usage statistics, only if the user ran `routengine stats on`; the window apps do the same.
	telemetry.Logf = logx.Printf
	go telemetry.Open(telemetry.DefaultPath()).Run(context.Background(), func() telemetry.Facts {
		f := telemetry.Facts{App: version, Edition: "free", Adapters: len(sched.Snapshot()), Routing: true}
		if l := license.Current(); l.Paid() {
			f.Edition, f.License = "pro", l.ID
		}
		path := c.rules
		if path == "" {
			path = policy.DefaultPath()
		}
		if rules, err := policy.LoadRules(path); err == nil {
			f.Rules = len(rules)
		}
		return f
	})
	if why := license.Tampered(); why != "" {
		logx.Printf("error: Start is blocked. %s Open RoutEngine itself, not this command, so it can check the license, or remove the license in its Settings.\n", why)
		os.Exit(1)
	}
	if err := t.On(); err != nil {
		logx.Printf("error: %v\n", err)
		os.Exit(1)
	}
	logx.Printf("RoutEngine is ON: all IPv4 traffic is balanced across the adapters above. Ctrl+C or `routengine off` to stop.\n")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	var crash, timeUp <-chan time.Time
	if *crashAfter > 0 {
		crash = time.After(*crashAfter)
	}
	if *runFor > 0 {
		timeUp = time.After(*runFor)
	}
	poll := time.NewTicker(300 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-sig:
		case <-timeUp:
		case <-crash:
			os.Exit(3) // simulates a crash: no teardown runs
		case <-poll.C:
			if !state.TakeMarker(c.stateDir, "off") && !state.TakeMarker(c.stateDir, "quit") {
				continue
			}
		}
		break
	}
	t.Close()
	engine.Apps.Flush() // save the data-used history
	state.Clear(c.stateDir)
	logx.Printf("RoutEngine is OFF.\n")
}

// cmdControl implements `on`, `off` and `quit` against a running tray/run instance.
func cmdControl(name string, args []string) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	dir := fs.String("state-dir", state.DefaultDir(), "state directory")
	fs.Parse(args)
	if !platform.InstanceRunning() {
		if name == "off" || name == "quit" {
			fmt.Println("RoutEngine is not running (already off).")
			return
		}
		die(fmt.Errorf("RoutEngine is not running; start it with `routengine tray`"))
	}
	_, wasOn := state.Read(*dir)
	if err := state.SendMarker(*dir, name); err != nil {
		die(err)
	}
	want := name == "on"
	for i := 0; i < 60; i++ {
		time.Sleep(150 * time.Millisecond)
		_, on := state.Read(*dir)
		switch {
		case name == "on" && on == want:
			fmt.Println("RoutEngine is ON.")
			return
		case name != "on" && !on:
			fmt.Println("RoutEngine is OFF.")
			return
		}
	}
	_ = wasOn
	die(fmt.Errorf("sent %q but it did not take effect (does this instance support it? run: routengine tray)", name))
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	dir := fs.String("state-dir", state.DefaultDir(), "state directory")
	fs.Parse(args)
	st, on := state.Read(*dir)
	if !on {
		fmt.Println("RoutEngine is OFF.")
		return
	}
	fmt.Printf("RoutEngine is ON (%s, pid %d, up %s)\nIPv6: %s\n%s", st.Mode, st.PID, time.Since(st.Since).Truncate(time.Second), st.V6, formatNICs(st.NICs))
}

func cmdCleanup() {
	if !platform.IsAdmin() {
		if err := platform.RelaunchElevated([]string{"cleanup"}, false); err != nil {
			die(err)
		}
		return
	}
	tun.CleanupStale()
	fmt.Println("done.")
}
