package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/recipes"
)

// Lister is the seam over launchd.ListRunningServices.
type Lister interface {
	ListRunningServices(dir string) ([]launchd.RunningService, error)
}

// LaunchdLister is the production adapter.
type LaunchdLister struct{}

func (LaunchdLister) ListRunningServices(dir string) ([]launchd.RunningService, error) {
	return launchd.ListRunningServices(dir)
}

// ProcInspector queries kernel state per pid (uptime, RSS).
type ProcInspector interface {
	RSS(pid int) (int64, error)
	Uptime(pid int) (time.Duration, error)
}

// PIDResolver returns the live PID for a launchd label, or 0 when the
// service isn't loaded. Wraps `launchctl print`.
type PIDResolver interface {
	Print(ctx context.Context, label string) (launchd.ServiceInfo, error)
}

// Poller scrapes each running backend on a tick. Fields must be set
// before Run is called. LaunchdService and ProcInspector are optional;
// when nil the response's memory_bytes and uptime_seconds remain zero.
type Poller struct {
	State          *State
	Lister         Lister
	LaunchdService PIDResolver
	ProcInspector  ProcInspector
	PlistDir       string
	HTTPClient     *http.Client
	Interval       time.Duration
	// BaseURLFn lets tests redirect requests to httptest URLs while
	// production builds use http://127.0.0.1:<port>.
	BaseURLFn func(port int) string
}

// Run blocks until ctx is canceled, ticking every Interval.
func (p *Poller) Run(ctx context.Context) {
	if p.Interval <= 0 {
		p.Interval = 2 * time.Second
	}
	p.tickOnce(ctx) // immediate first scrape so /v1/telemetry isn't empty
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tickOnce(ctx)
		}
	}
}

// tickOnce runs one poll cycle: enumerate, fan out scrape, update state,
// forget disappeared IDs.
func (p *Poller) tickOnce(ctx context.Context) {
	services, err := p.Lister.ListRunningServices(p.PlistDir)
	if err != nil {
		return // transient; next tick will retry
	}
	known := make(map[string]bool, len(services))
	var wg sync.WaitGroup
	for _, svc := range services {
		known[svc.ID] = true
		wg.Add(1)
		go func(svc launchd.RunningService) {
			defer wg.Done()
			scrapeCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
			defer cancel()
			base := p.BaseURLFn(svc.Port)
			sample := Scrape(scrapeCtx, p.HTTPClient, base, svc.Port)
			sample.Recipe = recipes.IdentifyFromArgv(svc.Args)

			// Per-PID enrichment (memory + uptime). Skipped when
			// LaunchdService or ProcInspector is nil — keeps the
			// tests that use only fakeLister working.
			if p.LaunchdService != nil && p.ProcInspector != nil {
				label := "com.llamactl." + svc.ID
				if info, err := p.LaunchdService.Print(scrapeCtx, label); err == nil && info.PID > 0 {
					if rss, err := p.ProcInspector.RSS(info.PID); err == nil {
						sample.MemoryBytes = rss
					}
					if uptime, err := p.ProcInspector.Uptime(info.PID); err == nil {
						sample.UptimeSeconds = int64(uptime.Seconds())
					}
				}
			}

			p.State.Update(svc.ID, sample)
		}(svc)
	}
	wg.Wait()
	for _, id := range p.State.IDs() {
		if !known[id] {
			p.State.Forget(id)
		}
	}
}

// DefaultBaseURL is the production BaseURLFn — points at 127.0.0.1.
func DefaultBaseURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}
