// version.go -- build-time version stamp, read at runtime.
//
// version is overridden at build time by build.sh:
//
//	-ldflags "-X goc.version=$(git describe --tags --always)"
//
// Untagged builds fall back to the short revision (via the VCS info Go embeds
// at build time), and builds outside a git tree report "dev". versionInfo()
// is assembled at runtime so it can also carry the revision, date and a
// clean/dirty marker -- none of which exist as a single string in the binary.
package main

import (
	"runtime/debug"
	"strings"
)

var version = "dev"

func vcsInfo() (rev, committed string, modified bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", false
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			committed = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if len(committed) >= 10 {
		committed = committed[:10] // YYYY-MM-DD
	}
	return rev, committed, modified
}

func versionInfo() string {
	rev, committed, modified := vcsInfo()

	head := version
	if head == "" {
		head = rev // untagged build: the revision IS the identity
	}

	var extra []string
	// `git describe --tags` already carries the hash ("v0.0.1-3-ga1b2c3d");
	// printing it again is noise.
	if rev != "" && !strings.Contains(head, rev) {
		extra = append(extra, rev)
	}
	if committed != "" {
		extra = append(extra, committed)
	}
	if rev != "" {
		if modified {
			extra = append(extra, "dirty")
		} else {
			extra = append(extra, "clean")
		}
	}

	if head == "" {
		head = "dev"
	}
	if len(extra) == 0 {
		return head
	}
	return head + " (" + strings.Join(extra, ", ") + ")"
}
