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
