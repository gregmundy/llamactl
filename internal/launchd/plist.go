// Package launchd renders LaunchAgent plists and wraps `launchctl` for
// loading, unloading, and querying llamactl-managed services.
package launchd

import (
	"bytes"
	"fmt"
	"text/template"
)

// PlistSpec captures every field that varies between llamactl services.
// All other plist contents are fixed by the template.
type PlistSpec struct {
	Label       string   // e.g. "com.llamactl.qwen2.5-7b-instruct"
	LlamaServer string   // absolute path to the resolved llama-server binary
	Args        []string // argv from recipes.FlagsFor (NOT including LlamaServer itself)
	LogPath     string   // ~/Library/Logs/llamactl/<id>.log
	WorkingDir  string   // user home
}

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{xml .Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{xml .LlamaServer}}</string>
{{range .Args}}    <string>{{xml .}}</string>
{{end}}  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>WorkingDirectory</key>
  <string>{{xml .WorkingDir}}</string>
  <key>StandardOutPath</key>
  <string>{{xml .LogPath}}</string>
  <key>StandardErrorPath</key>
  <string>{{xml .LogPath}}</string>
  <key>ProcessType</key>
  <string>Interactive</string>
</dict>
</plist>
`

// xmlEscape replaces &, <, > with their XML entities. The plist values
// are strings; no need for full XML entity coverage (quotes don't appear
// inside <string> bodies).
func xmlEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var plistTpl = template.Must(template.New("plist").Funcs(template.FuncMap{
	"xml": xmlEscape,
}).Parse(plistTemplate))

// Render returns the rendered plist bytes for spec.
func Render(spec PlistSpec) ([]byte, error) {
	var buf bytes.Buffer
	if err := plistTpl.Execute(&buf, spec); err != nil {
		return nil, fmt.Errorf("render plist: %w", err)
	}
	return buf.Bytes(), nil
}
