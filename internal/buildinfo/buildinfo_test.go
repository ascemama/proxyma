package buildinfo

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		commit      string
		dirty       bool
		wantVersion string
		wantCommit  string
	}{
		{name: "release", version: "v1.2.3", commit: "ABCDEF012345", wantVersion: "1.2.3", wantCommit: "abcdef0"},
		{name: "prerelease", version: "v1.2.3-rc.1", commit: "abcdef012345", wantVersion: "1.2.3-rc.1", wantCommit: "abcdef0"},
		{name: "after tag", version: "v0.1.0-rc.1-4-g2396d74", commit: "unused", wantVersion: "0.1.0-rc.1+4.g2396d74", wantCommit: "2396d74"},
		{name: "untagged", version: "2396d74", wantVersion: "dev+g2396d74", wantCommit: "2396d74"},
		{name: "local metadata", commit: "2396d7487654", wantVersion: "dev+g2396d74", wantCommit: "2396d74"},
		{name: "container development build", version: "dev", commit: "2396d7487654", wantVersion: "dev+g2396d74", wantCommit: "2396d74"},
		{name: "dirty after tag", version: "v1.2.3-2-gabcdef0-dirty", wantVersion: "1.2.3+2.gabcdef0.dirty", wantCommit: "abcdef0"},
		{name: "dirty release", version: "v1.2.3", commit: "abcdef0", dirty: true, wantVersion: "1.2.3+dirty", wantCommit: "abcdef0"},
		{name: "unknown", wantVersion: "dev", wantCommit: "unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normalize(test.version, test.commit, test.dirty)
			if got.Version != test.wantVersion || got.Commit != test.wantCommit {
				t.Fatalf("normalize(%q, %q, %t) = %#v, want version=%q commit=%q", test.version, test.commit, test.dirty, got, test.wantVersion, test.wantCommit)
			}
		})
	}
}
