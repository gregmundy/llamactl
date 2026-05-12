package cli

import (
	"context"

	"github.com/gregmundy/llamactl/internal/launchd"
)

// LaunchdServiceAdapter wraps *launchd.Service so it satisfies the
// LaunchdService interface. The List method needs the agents dir, which
// the wrapper closes over so callers don't have to thread it through.
type LaunchdServiceAdapter struct {
	Service   *launchd.Service
	AgentsDir string
}

func (a *LaunchdServiceAdapter) Load(ctx context.Context, plistPath string) error {
	return a.Service.Load(ctx, plistPath)
}

func (a *LaunchdServiceAdapter) Bootout(ctx context.Context, label string) error {
	return a.Service.Bootout(ctx, label)
}

func (a *LaunchdServiceAdapter) Print(ctx context.Context, label string) (launchd.ServiceInfo, error) {
	return a.Service.Print(ctx, label)
}

func (a *LaunchdServiceAdapter) List(ctx context.Context) ([]launchd.ServiceInfo, error) {
	return launchd.ListLLMServices(ctx, a.AgentsDir, a.Service)
}

// Compile-time assertion: LaunchdServiceAdapter must satisfy LaunchdService.
var _ LaunchdService = (*LaunchdServiceAdapter)(nil)
