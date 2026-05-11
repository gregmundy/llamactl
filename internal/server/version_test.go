package server

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantBuild int
		wantSHA   string
		wantErr   bool
	}{
		{
			name:      "standard llama.cpp output",
			input:     "version: 4567 (a1b2c3d4)\nbuilt with Apple clang version 15.0.0\n",
			wantBuild: 4567,
			wantSHA:   "a1b2c3d4",
		},
		{
			name:      "b-prefixed tag form",
			input:     "version: b4567 (a1b2c3d4)\n",
			wantBuild: 4567,
			wantSHA:   "a1b2c3d4",
		},
		{
			name:    "garbage",
			input:   "not a version",
			wantErr: true,
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := ParseVersion(c.input)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", v)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVersion: %v", err)
			}
			if v.Build != c.wantBuild {
				t.Errorf("Build = %d, want %d", v.Build, c.wantBuild)
			}
			if v.SHA != c.wantSHA {
				t.Errorf("SHA = %q, want %q", v.SHA, c.wantSHA)
			}
		})
	}
}

func TestVersion_AtLeast(t *testing.T) {
	v := Version{Build: 4500}
	if !v.AtLeast(4000) {
		t.Error("4500 >= 4000 should be true")
	}
	if v.AtLeast(5000) {
		t.Error("4500 >= 5000 should be false")
	}
}
