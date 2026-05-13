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
		return ArchMistral
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
