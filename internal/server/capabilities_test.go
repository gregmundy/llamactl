package server

import "testing"

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
