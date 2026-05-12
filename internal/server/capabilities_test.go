package server

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestParseHelpForCaps_Tristate(t *testing.T) {
	// Recent llama-server help output (b1 d05fe1d, late 2026).
	help := `usage: llama-server [options]

-fa,   --flash-attn [on|off|auto]       set Flash Attention use ('on', 'off', or 'auto', default: 'auto')
                                        (env: LLAMA_ARG_FLASH_ATTN)
`
	caps := parseHelpForCaps(help)
	if !caps.FlashAttnTristate {
		t.Errorf("FlashAttnTristate = false, want true")
	}
}

func TestParseHelpForCaps_LegacyBoolean(t *testing.T) {
	// Pre-tristate llama-server help (Homebrew b4500 era).
	help := `usage: llama-server [options]

  -fa, --flash-attn                       enable Flash Attention (default: disabled)
`
	caps := parseHelpForCaps(help)
	if caps.FlashAttnTristate {
		t.Errorf("FlashAttnTristate = true, want false (legacy bare-flag syntax)")
	}
}

func TestParseHelpForCaps_FlashAttnAbsent(t *testing.T) {
	// Hypothetical build with no flash-attn flag at all.
	help := `usage: llama-server [options]

  -h, --help                              print usage
`
	caps := parseHelpForCaps(help)
	if caps.FlashAttnTristate {
		t.Errorf("FlashAttnTristate = true on help with no flash-attn line at all")
	}
}

type capsFakeRunner struct {
	helpOut string
	helpErr error
	calls   int
}

func (r *capsFakeRunner) Run(_ context.Context, name string, args []string, _ string, stdout, _ io.Writer) error {
	r.calls++
	if len(args) > 0 && args[0] == "--help" {
		if r.helpErr != nil {
			return r.helpErr
		}
		_, _ = io.WriteString(stdout, r.helpOut)
		return nil
	}
	return errors.New("unexpected args")
}

func TestProberCapabilitiesParsesAndCaches(t *testing.T) {
	r := &capsFakeRunner{helpOut: "--flash-attn [on|off|auto] do thing\n"}
	p := &Prober{Runner: r}

	caps, err := p.Capabilities(context.Background(), "/x/llama-server")
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if !caps.FlashAttnTristate {
		t.Errorf("FlashAttnTristate = false, want true")
	}
	if r.calls != 1 {
		t.Errorf("calls = %d, want 1", r.calls)
	}

	// Second call: should be cached, no extra runner invocation.
	_, _ = p.Capabilities(context.Background(), "/x/llama-server")
	if r.calls != 1 {
		t.Errorf("after 2nd call, calls = %d, want 1 (cache miss)", r.calls)
	}
}

func TestProberCapabilitiesRunnerError(t *testing.T) {
	r := &capsFakeRunner{helpErr: errors.New("boom")}
	p := &Prober{Runner: r}
	_, err := p.Capabilities(context.Background(), "/x/llama-server")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want wraps 'boom'", err)
	}
}
