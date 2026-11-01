package procmap

/*
#include <libproc.h>
#include <sys/proc_info.h>
#include <arpa/inet.h>
#include <stdlib.h>
#include <string.h>

// owner finds the process with a TCP or UDP socket whose local end is addr:port (addr is 4 or 16
// network-order bytes, or NULL for a wildcard match). It returns the pid, or 0.
static int owner(int tcp, const unsigned char *addr, int alen, unsigned short port) {
	int bytes = proc_listallpids(NULL, 0);
	if (bytes <= 0) return 0;
	int *pids = malloc(bytes * 2);
	if (!pids) return 0;
	int n = proc_listallpids(pids, bytes * 2) / sizeof(int);
	int found = 0;
	for (int i = 0; i < n && !found; i++) {
		int fb = proc_pidinfo(pids[i], PROC_PIDLISTFDS, 0, NULL, 0);
		if (fb <= 0) continue;
		struct proc_fdinfo *fds = malloc(fb);
		if (!fds) continue;
		int nf = proc_pidinfo(pids[i], PROC_PIDLISTFDS, 0, fds, fb) / sizeof(struct proc_fdinfo);
		for (int j = 0; j < nf && !found; j++) {
			if (fds[j].proc_fdtype != PROX_FDTYPE_SOCKET) continue;
			struct socket_fdinfo si;
			if (proc_pidfdinfo(pids[i], fds[j].proc_fd, PROC_PIDFDSOCKETINFO, &si, sizeof si) != sizeof si) continue;
			struct in_sockinfo *in;
			if (tcp && si.psi.soi_kind == SOCKINFO_TCP) in = &si.psi.soi_proto.pri_tcp.tcpsi_ini;
			else if (!tcp && si.psi.soi_kind == SOCKINFO_IN) in = &si.psi.soi_proto.pri_in;
			else continue;
			if (in->insi_lport != htons(port)) continue;
			if (addr == NULL) { found = pids[i]; break; }
			if (alen == 4 && (in->insi_vflag & INI_IPV4) && memcmp(&in->insi_laddr.ina_46.i46a_addr4, addr, 4) == 0) found = pids[i];
			if (alen == 16 && (in->insi_vflag & INI_IPV6) && memcmp(&in->insi_laddr.ina_6, addr, 16) == 0) found = pids[i];
		}
		free(fds);
	}
	free(pids);
	return found;
}
*/
import "C"

import (
	"net/netip"
	"sync"
	"unsafe"
)

// Supported reports whether this platform can map a flow to its process.
const Supported = true

var (
	mu    sync.Mutex
	exeOf = map[int]string{} // pid -> path; a pid's program rarely changes in the seconds it is cached
)

// Lookup names the process whose socket has src as its local end. It needs root to see other users' processes.
// ponytail: walks every process's descriptors per new flow; a PF_SYSTEM/pcblist index if that gets hot.
func Lookup(tcp bool, src netip.AddrPort) (Proc, bool) {
	a := src.Addr().Unmap()
	b := a.AsSlice()
	t := C.int(0)
	if tcp {
		t = 1
	}
	pid := int(C.owner(t, (*C.uchar)(unsafe.Pointer(&b[0])), C.int(len(b)), C.ushort(src.Port())))
	if pid == 0 && !tcp { // an unbound UDP socket that sent shows the wildcard address
		pid = int(C.owner(t, nil, 0, C.ushort(src.Port())))
	}
	if pid <= 0 {
		return Proc{}, false
	}
	return Proc{PID: uint32(pid), Exe: path(pid)}, true
}

func path(pid int) string {
	mu.Lock()
	defer mu.Unlock()
	if p, ok := exeOf[pid]; ok {
		return p
	}
	buf := make([]byte, C.PROC_PIDPATHINFO_MAXSIZE)
	n := int(C.proc_pidpath(C.int(pid), unsafe.Pointer(&buf[0]), C.uint32_t(len(buf))))
	p := ""
	if n > 0 {
		p = string(buf[:n])
	}
	if len(exeOf) > 4096 {
		clear(exeOf)
	}
	exeOf[pid] = p
	return p
}
