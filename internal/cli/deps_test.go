package cli

import "testing"

func TestDepsHasPhase2Fields(t *testing.T) {
	d := Deps{}
	// Compile-time checks: these field accesses must compile.
	_ = d.HFClient
	_ = d.Downloader
	_ = d.QuantSelector
	_ = d.ModelStore
	_ = d.FS
	_ = d.ModelsConfigDir
	_ = d.SharedModelsDir
	_ = d.HFCacheDir
}
