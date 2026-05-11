// Package platform answers host-environment questions the cli flows care
// about. Backed by stdlib runtime; mock in tests by satisfying the Platform
// interface defined in internal/cli/deps.go.
package platform

import "runtime"

// Default is the production Platform. The cli package consumes it via the
// Platform interface; tests substitute their own.
type Default struct{}

func (Default) IsAppleSilicon() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

func (Default) Cores() int { return runtime.NumCPU() }
