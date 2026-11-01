package procmap

import (
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Supported reports whether this platform can map a flow to its process.
const Supported = true

var (
	mu    sync.Mutex
	owner = map[uint64]Proc{} // socket inode -> process; an inode is never reused while its socket lives
)

// Lookup names the process whose socket has src as its local end. A process of another user is only
// visible with root; with just CAP_NET_ADMIN those flows stay unidentified.
func Lookup(tcp bool, src netip.AddrPort) (Proc, bool) {
	files := []string{"/proc/net/udp", "/proc/net/udp6"}
	if tcp {
		files = []string{"/proc/net/tcp", "/proc/net/tcp6"}
	}
	var inode uint64
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		n, ok := findInode(f, src, !tcp)
		f.Close()
		if ok {
			inode = n
			break
		}
	}
	if inode == 0 {
		return Proc{}, false
	}
	mu.Lock()
	p, ok := owner[inode]
	mu.Unlock()
	if ok {
		return p, true
	}
	// ponytail: scans every process's fds on a cache miss; sock_diag + a cached fd index if that gets hot.
	if p, ok = scan(inode); !ok {
		return Proc{}, false
	}
	mu.Lock()
	if len(owner) > 8192 {
		clear(owner)
	}
	owner[inode] = p
	mu.Unlock()
	return p, true
}

func scan(inode uint64) (Proc, bool) {
	want := "socket:[" + strconv.FormatUint(inode, 10) + "]"
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return Proc{}, false
	}
	for _, d := range procs {
		pid, err := strconv.ParseUint(d.Name(), 10, 32)
		if err != nil {
			continue
		}
		fds, err := os.ReadDir("/proc/" + d.Name() + "/fd")
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if l, err := os.Readlink("/proc/" + d.Name() + "/fd/" + fd.Name()); err == nil && l == want {
				return Proc{PID: uint32(pid), Exe: exeOf(d.Name())}, true
			}
		}
	}
	return Proc{}, false
}

// exeOf is the program's path; where /proc/<pid>/exe is off limits, its short name still lets a rule
// written as a bare name match.
func exeOf(pid string) string {
	if p, err := os.Readlink("/proc/" + pid + "/exe"); err == nil {
		return strings.TrimSuffix(p, " (deleted)")
	}
	if b, err := os.ReadFile("/proc/" + pid + "/comm"); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}
