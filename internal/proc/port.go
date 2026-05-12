// Package proc provides host-process introspection helpers used by
// `serve`, `status`, and `doctor`. Each module (port, ps, logtail) is
// independent and testable in isolation.
package proc

import (
	"fmt"
	"net"
)

// FreePort returns preferred if it's currently bindable, or the next
// free port in [preferred, preferred+100). preferred=0 asks the kernel
// for an ephemeral port.
func FreePort(preferred int) (int, error) {
	if preferred == 0 {
		l, err := net.Listen("tcp", ":0")
		if err != nil {
			return 0, fmt.Errorf("kernel-assigned port: %w", err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		return port, nil
	}
	for p := preferred; p < preferred+100; p++ {
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

func (Allocator) Free(preferred int) (int, error) { return FreePort(preferred) }
