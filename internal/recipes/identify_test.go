package recipes

import (
	"fmt"
	"testing"
)

func TestIdentifyFromArgv_AllRecipes(t *testing.T) {
	for name, r := range Recipes {
		argv := []string{
			"--ctx-size", fmt.Sprintf("%d", r.CtxSize),
			"--cache-type-k", r.CacheTypeK,
			"--cache-type-v", r.CacheTypeV,
		}
		if r.Reasoning != "" {
			argv = append(argv, "--reasoning", r.Reasoning)
		}
		got := IdentifyFromArgv(argv)
		if got != name {
			t.Errorf("recipe %q: IdentifyFromArgv = %q", name, got)
		}
	}
}

func TestIdentifyFromArgv_ClampedCtxSizeStillMatches(t *testing.T) {
	// long-context normally has 32768 ctx, but a model with MaxCtx=4096
	// gets clamped. The cache-type combo (q8_0/q8_0) is unique to
	// long-context so we should still match.
	argv := []string{
		"--ctx-size", "4096",
		"--cache-type-k", "q8_0",
		"--cache-type-v", "q8_0",
	}
	if got := IdentifyFromArgv(argv); got != "long-context" {
		t.Errorf("got %q, want long-context", got)
	}
}

func TestIdentifyFromArgv_NoMatchReturnsEmpty(t *testing.T) {
	argv := []string{
		"--ctx-size", "16384",
		"--cache-type-k", "iq4_nl",
		"--cache-type-v", "iq4_nl",
	}
	if got := IdentifyFromArgv(argv); got != "" {
		t.Errorf("got %q, want empty (no recipe matches)", got)
	}
}

func TestIdentifyFromArgv_DistinguishesAgentAndThinking(t *testing.T) {
	// agent and thinking share ctx/cache; --reasoning is the only
	// distinguishing flag.
	agentArgv := []string{
		"--ctx-size", "8192", "--cache-type-k", "f16", "--cache-type-v", "f16",
		"--reasoning", "off",
	}
	thinkingArgv := []string{
		"--ctx-size", "8192", "--cache-type-k", "f16", "--cache-type-v", "f16",
		"--reasoning", "on",
	}
	if got := IdentifyFromArgv(agentArgv); got != "agent" {
		t.Errorf("agent argv → %q", got)
	}
	if got := IdentifyFromArgv(thinkingArgv); got != "thinking" {
		t.Errorf("thinking argv → %q", got)
	}
}
