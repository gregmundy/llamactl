package launchd

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const labelPrefix = "com.llamactl."

// ListLLMServices scans agentsDir for plist files whose basename starts
// with "com.llamactl.". For each, calls svc.Print to populate PID/State.
// Missing directory returns nil, nil (no services).
func ListLLMServices(ctx context.Context, agentsDir string, svc *Service) ([]ServiceInfo, error) {
	entries, err := os.ReadDir(agentsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []ServiceInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".plist") {
			continue
		}
		if !strings.HasPrefix(name, labelPrefix) {
			continue
		}
		label := strings.TrimSuffix(name, ".plist")
		info, _ := svc.Print(ctx, label)
		info.Label = label
		info.PlistPath = filepath.Join(agentsDir, name)
		out = append(out, info)
	}
	return out, nil
}
