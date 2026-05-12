package server

import "strings"

// Capabilities is the per-binary feature surface detected from
// `llama-server --help`. Each field gates a recipe-emission decision.
type Capabilities struct {
	// FlashAttnTristate is true when --help shows the modern tristate
	// signature `--flash-attn [on|off|auto]`. Pre-tristate llama.cpp
	// builds accept a bare `--flash-attn` flag and would error with
	// "expected value" if we passed `--flash-attn on`.
	FlashAttnTristate bool
}

// parseHelpForCaps extracts capability flags from the combined
// stdout+stderr of `llama-server --help`. Best-effort: any field whose
// signal isn't found stays at its zero value.
func parseHelpForCaps(help string) Capabilities {
	var c Capabilities
	// Look for the tristate signature anywhere in the help text.
	// Pre-tristate help shows just "--flash-attn" followed by description.
	// Modern help shows "--flash-attn [on|off|auto]".
	for _, line := range strings.Split(help, "\n") {
		if !strings.Contains(line, "--flash-attn") {
			continue
		}
		if strings.Contains(line, "[on|off|auto]") || strings.Contains(line, "[on") {
			c.FlashAttnTristate = true
			break
		}
	}
	return c
}
