package proc

import (
	"fmt"
	"net"
	"testing"
)

func TestFreePortReturnsPreferredWhenAvailable(t *testing.T) {
	// Find a known-free port by binding+releasing, then ask FreePort for it.
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	preferred := l.Addr().(*net.TCPAddr).Port
	l.Close()

	got, err := FreePort(preferred, nil)
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}
	if got != preferred {
		t.Errorf("got %d, want %d (preferred should be returned when free)", got, preferred)
	}
}

func TestFreePortFallsThroughOnConflict(t *testing.T) {
	// Bind preferred port for the duration of the test.
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	preferred := l.Addr().(*net.TCPAddr).Port

	got, err := FreePort(preferred, nil)
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}
	if got == preferred {
		t.Errorf("got %d, want anything but %d", got, preferred)
	}
	if got < preferred || got >= preferred+100 {
		t.Errorf("got %d, want value in [%d, %d)", got, preferred, preferred+100)
	}
}

func TestFreePortSkipsListed(t *testing.T) {
	// Find a free starting port, ask for it normally — should be returned.
	p, err := FreePort(8080, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Now request the same starting port but skip the one we just got.
	p2, err := FreePort(p, []int{p})
	if err != nil {
		t.Fatal(err)
	}
	if p2 == p {
		t.Fatalf("FreePort returned skipped port %d", p)
	}
	if p2 < p || p2 >= p+100 {
		t.Errorf("got %d, want value in [%d, %d)", p2, p, p+100)
	}
}

// TestFreePortSkipsActiveListener pins the v1.4.6 fix: even when
// net.Listen would (somehow) succeed against a port another process is
// actively listening on, the dial-probe catches it and FreePort moves
// on. Holding a real listener for the duration of the test simulates
// the production scenario of llama-server holding 8082 while a second
// `serve --detach` invocation does its port scan.
func TestFreePortSkipsActiveListener(t *testing.T) {
	// Bind a real listener and keep it accepting.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	// Drain accepts so the listener stays in the "actively listening"
	// state — otherwise the kernel might close the accept queue and
	// portInUse could see something different. We don't care about the
	// accepted connections; just close them.
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	// Sanity: portInUse should report this port as in use.
	if !portInUse(port) {
		t.Fatalf("portInUse(%d) = false, want true (a listener is active)", port)
	}

	// FreePort asked for the held port should not return it.
	got, err := FreePort(port, nil)
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}
	if got == port {
		t.Errorf("FreePort returned actively-listening port %d", port)
	}
}

// TestPortInUseFalseOnGenuinelyFreePort verifies the negative case:
// portInUse returns false for ports nobody is listening on. Belt-and-
// suspenders for the v1.4.6 stricter "only connect-success counts as
// in use" rule — earlier ECONNREFUSED-aware variants treated transient
// EINVAL states as in-use and poisoned 100-port scans.
func TestPortInUseFalseOnGenuinelyFreePort(t *testing.T) {
	// Bind+release to learn a port that was just free.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	// portInUse on the released port should be false (no active listener).
	// Tolerant of TIME_WAIT residue — only connect-success counts.
	if portInUse(port) {
		t.Errorf("portInUse(%d) = true, want false (port was just released)", port)
	}
}

func TestAllocatorImplementsPortAllocator(t *testing.T) {
	var a Allocator
	got, err := a.Free(0, nil) // 0 means "let kernel pick"
	if err != nil {
		t.Fatalf("Free: %v", err)
	}
	if got <= 0 {
		t.Errorf("got %d, want >0", got)
	}
	_ = fmt.Sprint(got) // silence unused-import nag if we add fmt later
}
