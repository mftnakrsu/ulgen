package version

import (
	"runtime"
	"runtime/debug"
	"testing"
)

func TestString(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{
			name: "release with revision",
			info: Info{Version: "0.1.0", Revision: "abc1234", GoVersion: "go1.24.7", OS: "linux", Arch: "amd64"},
			want: "0.1.0 (abc1234) go1.24.7 linux/amd64",
		},
		{
			name: "dev build with uncommitted changes",
			info: Info{Version: "dev", Revision: "abc1234", Modified: true, GoVersion: "go1.24.7", OS: "windows", Arch: "arm64"},
			want: "dev (abc1234-dirty) go1.24.7 windows/arm64",
		},
		{
			name: "no VCS information",
			info: Info{Version: "dev", GoVersion: "go1.24.7", OS: "darwin", Arch: "arm64"},
			want: "dev go1.24.7 darwin/arm64",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddVCS(t *testing.T) {
	var info Info
	info.addVCS([]debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
		{Key: "vcs.time", Value: "2026-10-11T12:00:00Z"},
		{Key: "vcs.modified", Value: "true"},
		{Key: "CGO_ENABLED", Value: "0"},
	})
	if info.Revision != "0123456" {
		t.Errorf("Revision = %q, want %q", info.Revision, "0123456")
	}
	if !info.Modified {
		t.Error("Modified = false, want true")
	}
}

func TestAddVCSWithoutVCS(t *testing.T) {
	var info Info
	info.addVCS([]debug.BuildSetting{{Key: "CGO_ENABLED", Value: "0"}})
	if info.Revision != "" || info.Modified {
		t.Errorf("got Revision=%q Modified=%v, want empty and false", info.Revision, info.Modified)
	}
}

func TestShortRevision(t *testing.T) {
	tests := map[string]string{
		"":                 "",
		"abc":              "abc",
		"abcdefg":          "abcdefg",
		"abcdefgh":         "abcdefg",
		"0123456789abcdef": "0123456",
	}
	for in, want := range tests {
		if got := shortRevision(in); got != want {
			t.Errorf("shortRevision(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCurrent(t *testing.T) {
	info := Current()
	if info.Version != Version {
		t.Errorf("Version = %q, want %q", info.Version, Version)
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.OS != runtime.GOOS || info.Arch != runtime.GOARCH {
		t.Errorf("OS/Arch = %s/%s, want %s/%s", info.OS, info.Arch, runtime.GOOS, runtime.GOARCH)
	}
}
