package launchd

import (
	"bytes"
	"fmt"
	"text/template"
)

// TelemetrydLabel is the launchd label for the telemetry sidecar.
const TelemetrydLabel = "com.llamactl.telemetryd"

// TelemetrydSpec captures the few values that vary between telemetryd
// plist instances. Configuration (port/host/interval/api_key) is read
// by the daemon from ~/.config/llamactl/config.yaml — not baked into
// the plist — so updating config requires `telemetry disable` then
// `enable`, which is acceptable for a sidecar.
type TelemetrydSpec struct {
	Label      string // always TelemetrydLabel
	BinaryPath string // absolute path to llamactl-telemetryd
	LogPath    string // ~/Library/Logs/llamactl/telemetryd.log
	WorkingDir string // user home
}

const telemetrydTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{xml .Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{xml .BinaryPath}}</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <false/>
  <key>WorkingDirectory</key>
  <string>{{xml .WorkingDir}}</string>
  <key>StandardOutPath</key>
  <string>{{xml .LogPath}}</string>
  <key>StandardErrorPath</key>
  <string>{{xml .LogPath}}</string>
  <key>ProcessType</key>
  <string>Background</string>
</dict>
</plist>
`

var telemetrydTpl = template.Must(template.New("telemetryd").Funcs(template.FuncMap{
	"xml": xmlEscape,
}).Parse(telemetrydTemplate))

// RenderTelemetryd returns the rendered plist bytes for spec.
func RenderTelemetryd(spec TelemetrydSpec) ([]byte, error) {
	var buf bytes.Buffer
	if err := telemetrydTpl.Execute(&buf, spec); err != nil {
		return nil, fmt.Errorf("render telemetryd plist: %w", err)
	}
	return buf.Bytes(), nil
}
