// Package version reports which build of Ülgen is running.
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Version is the release version, or "dev" for a build that is not a release.
// Release builds set it at link time:
//
//	go build -ldflags "-X github.com/mftnakrsu/ulgen/internal/version.Version=0.1.0" ./cmd/ulgen
var Version = "dev"

// Info describes a build of the program.
type Info struct {
	Version   string // Release version, or "dev".
	Revision  string // Short VCS revision, or "" when the build carries none.
	Modified  bool   // True when the working tree had uncommitted changes.
	GoVersion string // Go toolchain, for example "go1.24.7".
	OS        string // Target operating system, for example "linux".
	Arch      string // Target architecture, for example "amd64".
}

// Current returns the information embedded in the running binary.
//
// The revision is present only when the binary was built with "go build"
// inside a version-controlled checkout; "go run" and builds from a source
// archive carry none.
func Current() Info {
	info := Info{
		Version:   Version,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		info.addVCS(bi.Settings)
	}
	return info
}

// addVCS fills Revision and Modified from the VCS settings of a build.
func (i *Info) addVCS(settings []debug.BuildSetting) {
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			i.Revision = shortRevision(s.Value)
		case "vcs.modified":
			i.Modified = s.Value == "true"
		}
	}
}

// shortRevision abbreviates a revision hash to its first seven characters.
func shortRevision(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}

// String formats the build as "dev (abc1234-dirty) go1.24.7 linux/amd64".
// The parenthesised part is omitted when the revision is unknown.
func (i Info) String() string {
	var b strings.Builder
	b.WriteString(i.Version)
	if i.Revision != "" {
		b.WriteString(" (")
		b.WriteString(i.Revision)
		if i.Modified {
			b.WriteString("-dirty")
		}
		b.WriteString(")")
	}
	b.WriteString(" ")
	b.WriteString(i.GoVersion)
	b.WriteString(" ")
	b.WriteString(i.OS)
	b.WriteString("/")
	b.WriteString(i.Arch)
	return b.String()
}
