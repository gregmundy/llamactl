package models

// ArchFromGGUF maps the value of GGUF's general.architecture metadata key
// to llamactl's Arch type. Recognised values map to canonical Archs;
// unknown values pass through verbatim (the selector will not match them,
// but they're stored in Metadata.Arch for `list` display and future use).
func ArchFromGGUF(s string) Arch {
	switch s {
	case "llama":
		return ArchLlama3
	case "mistral":
		// Non-standard: real-world Mistral GGUFs report "llama". This case
		// handles the rare GGUF that explicitly reports "mistral" so it
		// still maps to the Llama-family Arch (matching KV-cache + params
		// formulas).
		return ArchLlama3
	case "qwen2":
		// Qwen 2 and Qwen 2.5 both emit general.architecture="qwen2" — the
		// ArchQwen25 constant carries the same string. Map explicitly so the
		// intent is clear and the constant remains the canonical reference.
		return ArchQwen25
	case "qwen3":
		return ArchQwen3
	case "gemma3":
		return ArchGemma3
	case "gemma4":
		return ArchGemma4
	default:
		return Arch(s)
	}
}
