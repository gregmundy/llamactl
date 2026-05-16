package telemetry

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// MetricsValues is the subset of llama.cpp's /metrics output we use.
type MetricsValues struct {
	TokensPredictedTotal   uint64
	TokensPredictedSeconds float64
	RequestsProcessing     float64
}

// ParseMetrics extracts known series from a Prometheus text-format body.
// Unknown lines, malformed values, and missing series are tolerated
// silently — the goal is a best-effort snapshot, not strict validation.
func ParseMetrics(body string) (MetricsValues, error) {
	var v MetricsValues
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if i := strings.Index(name, "{"); i >= 0 {
			name = name[:i]
		}
		valStr := fields[len(fields)-1]
		switch name {
		case "llamacpp:tokens_predicted_total":
			if n, err := strconv.ParseUint(valStr, 10, 64); err == nil {
				v.TokensPredictedTotal = n
			}
		case "llamacpp:tokens_predicted_seconds_total":
			if f, err := strconv.ParseFloat(valStr, 64); err == nil {
				v.TokensPredictedSeconds = f
			}
		case "llamacpp:requests_processing":
			if f, err := strconv.ParseFloat(valStr, 64); err == nil {
				v.RequestsProcessing = f
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return v, fmt.Errorf("scan metrics: %w", err)
	}
	return v, nil
}
