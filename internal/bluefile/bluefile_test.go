package bluefile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Bluefile")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseDefaultsAndOverrides(t *testing.T) {
	s, err := Parse(write(t, `
BASE     docker.io/library/alpine:latest
CPUS     4
RAM      4096
NETWORK  none
READONLY true
TIMEOUT  60
PACKAGE  ripgrep python3
PACKAGE  jq
RUN      pip3 install requests
ENV      LANG=C.UTF-8
`))
	if err != nil {
		t.Fatal(err)
	}
	if s.CPUs != 4 || s.RAMMiB != 4096 || s.Network != "none" {
		t.Errorf("core fields wrong: %+v", s)
	}
	if !s.ReadOnlyRootfs || s.TimeoutSeconds != 60 {
		t.Errorf("restriction fields wrong: %+v", s)
	}
	if got := strings.Join(s.Packages, ","); got != "ripgrep,python3,jq" {
		t.Errorf("packages accumulate wrong: %q", got)
	}
	if len(s.Run) != 1 || len(s.Env) != 1 {
		t.Errorf("run/env wrong: %+v", s)
	}
}

func TestValidation(t *testing.T) {
	cases := map[string]string{
		"cpus too high": "BASE x\nCPUS 99\n",
		"bad network":   "BASE x\nNETWORK wifi\n",
		"tiny ram":      "BASE x\nRAM 4\n",
		"bad bool":      "BASE x\nREADONLY yes-please\n",
		"unknown key":   "BASE x\nWIDGETS 3\n",
		"no value":      "BASE\n",
	}
	for name, body := range cases {
		if _, err := Parse(write(t, body)); err == nil {
			t.Errorf("%s: expected error, got none", name)
		}
	}
}

func TestPackageManagerSelection(t *testing.T) {
	cases := []struct{ base, want string }{
		{"docker.io/library/alpine:latest", "apk add --no-cache"},
		{"docker.io/library/debian:bookworm-slim", "apt-get install"},
		{"docker.io/library/ubuntu:24.04", "apt-get install"},
		{"docker.io/library/fedora:41", "dnf install"},
		{"quay.io/centos/centos:stream9", "dnf install"},
	}
	for _, c := range cases {
		s, err := Parse(write(t, "BASE "+c.base+"\nPACKAGE jq\n"))
		if err != nil {
			t.Errorf("%s: %v", c.base, err)
			continue
		}
		if cf := s.Containerfile(); !strings.Contains(cf, c.want) {
			t.Errorf("%s: want %q in:\n%s", c.base, c.want, cf)
		}
	}
}

func TestPackageManagerCleanup(t *testing.T) {
	apt, _ := Parse(write(t, "BASE docker.io/library/debian:12\nPACKAGE jq\n"))
	if !strings.Contains(apt.Containerfile(), "rm -rf /var/lib/apt/lists") {
		t.Error("apt should clean its lists")
	}
	dnf, _ := Parse(write(t, "BASE docker.io/library/fedora:41\nPACKAGE jq\n"))
	if !strings.Contains(dnf.Containerfile(), "dnf clean all") {
		t.Error("dnf should clean its cache")
	}
}

// An unrecognised base must fail at parse time rather than generate a
// Containerfile that dies partway through the build.
func TestUnknownBaseNeedsPkgmgr(t *testing.T) {
	if _, err := Parse(write(t, "BASE example.com/custom/image:1\nPACKAGE jq\n")); err == nil {
		t.Error("expected an error asking for PKGMGR")
	}
	// ...unless PKGMGR says which one to use.
	s, err := Parse(write(t, "BASE example.com/custom/image:1\nPKGMGR apt\nPACKAGE jq\n"))
	if err != nil {
		t.Fatalf("PKGMGR override should work: %v", err)
	}
	if !strings.Contains(s.Containerfile(), "apt-get install") {
		t.Errorf("PKGMGR override ignored:\n%s", s.Containerfile())
	}
	// No packages means no package manager is needed at all.
	if _, err := Parse(write(t, "BASE example.com/custom/image:1\n")); err != nil {
		t.Errorf("base with no packages should parse: %v", err)
	}
	if _, err := Parse(write(t, "BASE alpine\nPKGMGR yum\nPACKAGE jq\n")); err == nil {
		t.Error("expected an error for an unknown PKGMGR")
	}
}

func TestContainerfileStructure(t *testing.T) {
	s, _ := Parse(write(t, "BASE b\nENV K=V\nRUN echo hi\n"))
	cf := s.Containerfile()
	if !strings.HasPrefix(cf, "# generated") {
		t.Error("missing generated header")
	}
	if !strings.Contains(cf, "FROM b") || !strings.Contains(cf, "ENV K=V") ||
		!strings.Contains(cf, "RUN echo hi") || !strings.HasSuffix(cf, "WORKDIR /data\n") {
		t.Errorf("structure wrong:\n%s", cf)
	}
}

func TestCommentsAndBlankLines(t *testing.T) {
	s, err := Parse(write(t, "# comment\n\nBASE b   # trailing\nCPUS 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Base != "b" || s.CPUs != 3 {
		t.Errorf("comment handling wrong: %+v", s)
	}
}
