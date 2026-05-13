// Package proc provides host-process introspection helpers used by
// `serve`, `status`, and `doctor`. Each module (port, ps, logtail) is
// independent and testable in isolation.
package proc

import (
	"fmt"
	"net"
	"time"
)

// FreePort returns preferred if it's currently bindable (and not in
// skip), or the next free port in [preferred, preferred+100) that
// passes both checks. preferred=0 asks the kernel for an ephemeral
// port; skip is ignored in that case.
//
// skip lets callers exclude ports they know are already claimed by
// sibling processes that haven't bound yet — e.g. a freshly-bootstrapped
// launchd service whose child llama-server is still loading its model.
// Without this, two rapid `serve --detach` invocations on different
// models can race to allocate the same port.
func FreePort(preferred int, skip []int) (int, error) {
	if preferred == 0 {
		l, err := net.Listen("tcp", ":0")
		if err != nil {
			return 0, fmt.Errorf("kernel-assigned port: %w", err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		return port, nil
	}
	skipSet := make(map[int]bool, len(skip))
	for _, p := range skip {
		skipSet[p] = true
	}
	for p := preferred; p < preferred+100; p++ {
		if skipSet[p] {
			continue
		}
		l, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
		if err != nil {
			continue
		}
		_ = l.Close()
		// Belt-and-suspenders: on Darwin, net.Listen can succeed against
		// a port another process is actively listening on (SO_REUSEADDR
		// semantics interact with how non-Go processes like llama-server
		// hold their sockets). The v1.4.0 doctor fix called this out for
		// the port-conflict check but the fix wasn't mirrored here, so
		// rapid `serve --detach` invocations could occasionally hand the
		// same port to two services. The Dial-probe confirms nobody is
		// actually listening before we commit to returning the port.
		if portInUse(p) {
			continue
		}
		return p, nil
	}
	return 0, fmt.Errorf("no free port in [%d, %d)", preferred, preferred+100)
}

// portInUse returns true only when a TCP DialTimeout to 127.0.0.1:port
// actually connects — i.e. some process is currently listening and
// accepting. Any error (ECONNREFUSED, EINVAL on a port the kernel is
// still tearing down, timeout, etc.) is treated as "free": we can't
// reach a listener, so we won't conflict with one.
//
// This is intentionally narrower than doctor.go's portConflictsCheck,
// which distinguishes ECONNREFUSED (free) from other errors (ambiguous,
// treated as in-use). FreePort's caller is about to TRY to bind, so the
// only relevant signal is "is there an active listener right now?". A
// transient EINVAL during socket teardown shouldn't poison a 100-port
// scan — that's an artifact, not a real conflict.
//
// 127.0.0.1 is the right target even though llamactl services bind
// 0.0.0.0 — any actual listener on 0.0.0.0 also accepts on the loopback
// interface, so the loopback probe is sufficient and avoids resolving
// the host's external IP.
//
// 200ms is the timeout cap; an active local listener accepts SYN in
// microseconds. We pay the cap only when there's a firewall or other
// oddity stalling the connect, which is the case where "treat as free"
// is actually correct — if we can't talk to it, we can't conflict.
func portInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Allocator implements the cli.PortAllocator interface.
type Allocator struct{}

func (Allocator) Free(preferred int, skip []int) (int, error) {
	return FreePort(preferred, skip)
}
