// Package proc provides host-process introspection helpers used by
// `serve`, `status`, and `doctor`. Each module (port, ps, logtail) is
// independent and testable in isolation.
package proc

import (
	"fmt"
	"net"
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
		return p, nil
	}
	return 0, fmt.Errorf("no free port in [%d, %d)", preferred, preferred+100)
}

// Allocator implements the cli.PortAllocator interface.
type Allocator struct{}

func (Allocator) Free(preferred int, skip []int) (int, error) {
	return FreePort(preferred, skip)
}
