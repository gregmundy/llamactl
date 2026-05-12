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
	case "qwen3":
		return ArchQwen3
	default:
		return Arch(s)
	}
}
