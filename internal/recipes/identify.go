package recipes

import "fmt"

// IdentifyFromArgv returns the recipe Name whose recipe-defining flags
// match args, or "" when no recipe can be unambiguously identified.
//
// Two-pass match strategy:
//   1. Exact match on CtxSize + CacheTypeK + CacheTypeV + Reasoning.
//      Handles all 6 stock recipes when ctx isn't clamped.
//   2. Fallback on CacheTypeK + CacheTypeV + Reasoning when the cache
//      combo uniquely identifies a recipe. Handles the
//      "long-context clamped to a small MaxCtx" case (q8_0/q8_0 is
//      unique). When the fallback would be ambiguous (e.g. chat vs
//      code both serve f16/f16 with no reasoning), returns "".
//
// Used by the telemetry sidecar to populate the `recipe` field per
// running service. Best-effort by design — customized recipes and
// ambiguous clamps return "".
func IdentifyFromArgv(args []string) string {
	var ctxSize, ctk, ctv, reasoning string
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "--ctx-size":
			ctxSize = args[i+1]
		case "--cache-type-k":
			ctk = args[i+1]
		case "--cache-type-v":
			ctv = args[i+1]
		case "--reasoning":
			reasoning = args[i+1]
		}
	}
	// Pass 1: exact match.
	for name, r := range Recipes {
		if fmt.Sprintf("%d", r.CtxSize) == ctxSize &&
			r.CacheTypeK == ctk && r.CacheTypeV == ctv && r.Reasoning == reasoning {
			return name
		}
	}
	// Pass 2: unique cache+reasoning fallback.
	var matches []string
	for name, r := range Recipes {
		if r.CacheTypeK == ctk && r.CacheTypeV == ctv && r.Reasoning == reasoning {
			matches = append(matches, name)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}
