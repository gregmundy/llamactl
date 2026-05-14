package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newStatusCmd(d *Deps) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "List detached llamactl services",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), d, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

type statusRow struct {
	Name          string  `json:"name"`
	ModelID       string  `json:"model_id"`
	Port          int     `json:"port"`
	State         string  `json:"state"`
	PID           int     `json:"pid,omitempty"`
	MemoryBytes   int64   `json:"memory_bytes,omitempty"`
	UptimeSeconds int64   `json:"uptime_seconds,omitempty"`
	TokensPerSec  float64 `json:"tokens_per_sec,omitempty"`
	Endpoint      string  `json:"endpoint,omitempty"`
}

var (
	plistPortRe  = regexp.MustCompile(`(?s)<string>--port</string>\s*<string>(\d+)</string>`)
	plistModelRe = regexp.MustCompile(`(?s)<string>--model</string>\s*<string>([^<]+)</string>`)
)

func runStatus(ctx context.Context, d *Deps, asJSON bool) error {
	services, err := d.LaunchdService.List(ctx)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		if asJSON {
			fmt.Fprintln(d.Stdout, "[]")
		} else {
			fmt.Fprintln(d.Stdout, "no detached services")
		}
		return nil
	}

	rows := make([]statusRow, 0, len(services))
	for _, svc := range services {
		name := strings.TrimPrefix(svc.Label, "com.llamactl.")
		port := readPortFromPlist(svc.PlistPath)
		modelID := readModelIDFromPlist(svc.PlistPath)

		row := statusRow{Name: name, ModelID: modelID, Port: port}
		info, _ := d.LaunchdService.Print(ctx, svc.Label)
		if info.PID == 0 {
			row.State = "stopped"
		} else {
			row.State = "running"
			row.PID = info.PID
			if rss, err := d.ProcInspector.RSS(info.PID); err == nil {
				row.MemoryBytes = rss
			}
			if up, err := d.ProcInspector.Uptime(info.PID); err == nil {
				row.UptimeSeconds = int64(up.Seconds())
			}
			// Log file path is keyed by run name (matches runServeDetached).
			logPath := d.LogsDir + "/" + name + ".log"
			rate, _ := d.TokRateReader.Rate(logPath, time.Minute, time.Now())
			row.TokensPerSec = rate
			if port > 0 {
				row.Endpoint = fmt.Sprintf("http://localhost:%d", port)
			}
		}
		rows = append(rows, row)
	}

	if asJSON {
		out, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(d.Stdout, string(out))
		return nil
	}

	tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tMODEL-ID\tPORT\tSTATE\tMEM\tUPTIME\tTOK/S\tENDPOINT")
	for _, r := range rows {
		mem := "—"
		upStr := "—"
		toks := "—"
		ep := "—"
		if r.State == "running" {
			mem = humanFileSize(r.MemoryBytes)
			upStr = humanDuration(time.Duration(r.UptimeSeconds) * time.Second)
			if r.TokensPerSec > 0 {
				toks = fmt.Sprintf("%.1f t/s", r.TokensPerSec)
			}
			ep = r.Endpoint
		}
		modelDisplay := r.ModelID
		if modelDisplay == "" {
			// Plist didn't carry a parseable --model arg. Fall back to name
			// so the table cell isn't empty — typical when status reads a
			// plist from an older llamactl version or a manually-edited file.
			modelDisplay = "—"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
			r.Name, modelDisplay, r.Port, r.State, mem, upStr, toks, ep)
	}
	return tw.Flush()
}

// readModelIDFromPlist extracts the model id from the plist's `--model
// <path>` argument. The path follows the convention
//   ~/.local/share/llama-models/<id>/<quant>.gguf
// so the parent directory of the file IS the model id. Returns "" if
// the plist is missing, malformed, or the --model arg can't be parsed.
func readModelIDFromPlist(plistPath string) string {
	data, err := os.ReadFile(plistPath)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	if err != nil {
		return ""
	}
	m := plistModelRe.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return filepath.Base(filepath.Dir(string(m[1])))
}

func readPortFromPlist(plistPath string) int {
	data, err := os.ReadFile(plistPath)
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err != nil {
		return 0
	}
	m := plistPortRe.FindSubmatch(data)
	if m == nil {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(string(m[1]), "%d", &n)
	return n
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	return fmt.Sprintf("%dh%dm", h, m)
}
