// Package buildinfo reports the identity of the running Proxyma build.
package buildinfo

import (
	"regexp"
	"runtime/debug"
	"strings"
)

var (
	rawVersion string
	rawCommit  string

	exactVersionPattern     = regexp.MustCompile(`^v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$`)
	describedVersionPattern = regexp.MustCompile(`^v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)-([0-9]+)-g([0-9a-fA-F]+)$`)
	commitPattern           = regexp.MustCompile(`^[0-9a-fA-F]{7,}$`)
)

// Info is the version and source revision embedded in a Proxyma binary.
type Info struct {
	Version string
	Commit  string
}

// Current returns linker-injected build information when available, falling
// back to the VCS metadata embedded by the Go toolchain for local builds.
func Current() Info {
	version := strings.TrimSpace(rawVersion)
	commit := strings.TrimSpace(rawCommit)
	dirty := false

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if commit == "" {
					commit = setting.Value
				}
			case "vcs.modified":
				dirty = setting.Value == "true"
			}
		}
	}

	return normalize(version, commit, dirty)
}

func normalize(version, commit string, dirty bool) Info {
	version = strings.TrimSpace(version)
	if strings.HasSuffix(version, "-dirty") {
		version = strings.TrimSuffix(version, "-dirty")
		dirty = true
	}

	shortCommit := shortRevision(commit)
	if match := describedVersionPattern.FindStringSubmatch(version); match != nil {
		shortCommit = shortRevision(match[3])
		version = match[1] + "+" + match[2] + ".g" + shortCommit
	} else if match := exactVersionPattern.FindStringSubmatch(version); match != nil {
		version = match[1]
	} else if commitPattern.MatchString(version) {
		if shortCommit == "unknown" {
			shortCommit = shortRevision(version)
		}
		version = "dev+g" + shortCommit
	} else if version == "" || version == "(devel)" || version == "dev" {
		if shortCommit == "unknown" {
			version = "dev"
		} else {
			version = "dev+g" + shortCommit
		}
	} else {
		version = strings.TrimPrefix(version, "v")
	}

	if dirty {
		if strings.Contains(version, "+") {
			version += ".dirty"
		} else {
			version += "+dirty"
		}
	}
	return Info{Version: version, Commit: shortCommit}
}

func shortRevision(revision string) string {
	revision = strings.TrimSpace(revision)
	if len(revision) > 7 {
		revision = revision[:7]
	}
	if revision == "" {
		return "unknown"
	}
	return strings.ToLower(revision)
}
