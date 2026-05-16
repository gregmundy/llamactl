package telemetry

import "testing"

const sampleMetrics = `# HELP llamacpp:prompt_tokens_total Number of prompt tokens processed.
# TYPE llamacpp:prompt_tokens_total counter
llamacpp:prompt_tokens_total 52
# HELP llamacpp:tokens_predicted_total Number of generation tokens processed.
# TYPE llamacpp:tokens_predicted_total counter
llamacpp:tokens_predicted_total 60
# HELP llamacpp:tokens_predicted_seconds_total Predict process time
# TYPE llamacpp:tokens_predicted_seconds_total counter
llamacpp:tokens_predicted_seconds_total 0.255
# HELP llamacpp:requests_processing Number of requests processing.
# TYPE llamacpp:requests_processing gauge
llamacpp:requests_processing 1
`

func TestParseMetrics_Happy(t *testing.T) {
	v, err := ParseMetrics(sampleMetrics)
	if err != nil {
		t.Fatal(err)
	}
	if v.TokensPredictedTotal != 60 {
		t.Errorf("TokensPredictedTotal = %d, want 60", v.TokensPredictedTotal)
	}
	if v.TokensPredictedSeconds != 0.255 {
		t.Errorf("TokensPredictedSeconds = %v, want 0.255", v.TokensPredictedSeconds)
	}
	if v.RequestsProcessing != 1.0 {
		t.Errorf("RequestsProcessing = %v, want 1.0", v.RequestsProcessing)
	}
}

func TestParseMetrics_Empty(t *testing.T) {
	v, err := ParseMetrics("")
	if err != nil {
		t.Fatal(err)
	}
	if v.TokensPredictedTotal != 0 || v.TokensPredictedSeconds != 0 {
		t.Errorf("empty body should yield zero values, got %+v", v)
	}
}

func TestParseMetrics_Malformed(t *testing.T) {
	body := "llamacpp:tokens_predicted_total not-a-number\nllamacpp:requests_processing 2"
	v, err := ParseMetrics(body)
	if err != nil {
		t.Fatal(err)
	}
	if v.TokensPredictedTotal != 0 {
		t.Error("malformed value should leave field zero")
	}
	if v.RequestsProcessing != 2 {
		t.Errorf("RequestsProcessing = %v, want 2", v.RequestsProcessing)
	}
}

func TestParseMetrics_WithLabels(t *testing.T) {
	// Series with {label="x"} should still be parsed by name.
	body := `llamacpp:tokens_predicted_total{instance="a"} 99`
	v, err := ParseMetrics(body)
	if err != nil {
		t.Fatal(err)
	}
	if v.TokensPredictedTotal != 99 {
		t.Errorf("TokensPredictedTotal = %d, want 99", v.TokensPredictedTotal)
	}
}
