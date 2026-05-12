package models

import (
	"encoding/json"
	"testing"
)

func TestMetadataParamsBJSONBackwardsCompat(t *testing.T) {
	raw := []byte(`{"id":"foo","params_b":3,"arch":"qwen2.5","quant":"Q5_K_M"}`)
	var m Metadata
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.ParamsB != 3.0 {
		t.Fatalf("ParamsB=%v, want 3.0", m.ParamsB)
	}
}

func TestMetadataParamsBFractionalRoundTrip(t *testing.T) {
	m := Metadata{ID: "qwen3-0.6b", ParamsB: 0.6, Arch: "qwen3"}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var back Metadata
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.ParamsB != 0.6 {
		t.Fatalf("round-trip ParamsB=%v, want 0.6", back.ParamsB)
	}
}
