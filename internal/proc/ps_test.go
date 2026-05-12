package proc

import (
	"errors"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/testutil"
)

func TestRSSParsesKilobytesToBytes(t *testing.T) {
	r := &testutil.FakeRunner{
		Outputs: map[string]string{
			"ps -o rss= -p 12345": "  1234567\n",
		},
	}
	i := &Inspector{Runner: r}
	got, err := i.RSS(12345)
	if err != nil {
		t.Fatalf("RSS: %v", err)
	}
	if got != 1234567*1024 {
		t.Errorf("got %d, want %d", got, 1234567*1024)
	}
}

func TestRSSProcessNotFound(t *testing.T) {
	r := &testutil.FakeRunner{
		Errs: map[string]error{
			"ps -o rss= -p 99999": errors.New("exit 1"),
		},
	}
	i := &Inspector{Runner: r}
	_, err := i.RSS(99999)
	if !errors.Is(err, ErrProcessNotFound) {
		t.Errorf("err = %v, want ErrProcessNotFound", err)
	}
}

func TestUptimeMMSS(t *testing.T) {
	r := &testutil.FakeRunner{
		Outputs: map[string]string{
			"ps -o etime= -p 100": "05:23\n",
		},
	}
	i := &Inspector{Runner: r}
	got, err := i.Uptime(100)
	if err != nil {
		t.Fatalf("Uptime: %v", err)
	}
	want := 5*time.Minute + 23*time.Second
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestUptimeHHMMSS(t *testing.T) {
	r := &testutil.FakeRunner{Outputs: map[string]string{"ps -o etime= -p 100": "1:05:23\n"}}
	i := &Inspector{Runner: r}
	got, err := i.Uptime(100)
	if err != nil {
		t.Fatalf("Uptime: %v", err)
	}
	want := time.Hour + 5*time.Minute + 23*time.Second
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseEtimeMalformed(t *testing.T) {
	cases := []string{"abc:def", "12:34:wat", "1-bad:00:00", ""}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			_, err := parseEtime(s)
			if err == nil {
				t.Fatalf("parseEtime(%q): expected error, got nil", s)
			}
		})
	}
}

func TestUptimeDaysHHMMSS(t *testing.T) {
	r := &testutil.FakeRunner{Outputs: map[string]string{"ps -o etime= -p 100": "2-01:05:23\n"}}
	i := &Inspector{Runner: r}
	got, err := i.Uptime(100)
	if err != nil {
		t.Fatalf("Uptime: %v", err)
	}
	want := 2*24*time.Hour + time.Hour + 5*time.Minute + 23*time.Second
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}
