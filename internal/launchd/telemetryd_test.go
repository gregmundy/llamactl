package launchd

import (
	"strings"
	"testing"
)

func TestRenderTelemetryd_KeyFields(t *testing.T) {
	body, err := RenderTelemetryd(TelemetrydSpec{
		Label:      "com.llamactl.telemetryd",
		BinaryPath: "/opt/homebrew/bin/llamactl-telemetryd",
		LogPath:    "/Users/x/Library/Logs/llamactl/telemetryd.log",
		WorkingDir: "/Users/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"<string>com.llamactl.telemetryd</string>",
		"<string>/opt/homebrew/bin/llamactl-telemetryd</string>",
		"<key>KeepAlive</key>",
		"<key>ProcessType</key>",
		"<string>Background</string>",
		"<key>RunAtLoad</key>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("plist missing %q\n%s", want, s)
		}
	}
	// Must NOT have KeepAlive=true (telemetryd should not thrash-restart).
	if !strings.Contains(s, "KeepAlive") || !containsAfter(s, "KeepAlive", "<false/>") {
		t.Errorf("telemetryd plist should have KeepAlive=false\n%s", s)
	}
}

func TestTelemetrydLabelConstant(t *testing.T) {
	if TelemetrydLabel != "com.llamactl.telemetryd" {
		t.Errorf("TelemetrydLabel = %q", TelemetrydLabel)
	}
}

// containsAfter reports whether `marker` appears in s and `wanted`
// appears somewhere after that position.
func containsAfter(s, marker, wanted string) bool {
	i := strings.Index(s, marker)
	if i < 0 {
		return false
	}
	return strings.Contains(s[i:], wanted)
}
