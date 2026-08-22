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
base: docker.io/library/alpine:latest
cpus: 4
ram_mib: 4096
network: none
readonly: true
timeout_seconds: 60
packages:
  - ripgrep
  - python3
run:
  - pip3 install requests
env:
  LANG: C.UTF-8
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
	if got := strings.Join(s.Packages, ","); got != "ripgrep,python3" {
		t.Errorf("packages wrong: %q", got)
	}
	if len(s.Run) != 1 || s.Env["LANG"] != "C.UTF-8" {
		t.Errorf("run/env wrong: %+v", s)
	}
}

// Omitted keys fall back to Default rather than zero values.
func TestDefaultsApplyToOmittedKeys(t *testing.T) {
	s, err := Parse(write(t, "base: docker.io/library/alpine:latest\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.CPUs != Default.CPUs || s.RAMMiB != Default.RAMMiB || s.Network != Default.Network {
		t.Errorf("defaults not applied: %+v", s)
	}
}

func TestValidation(t *testing.T) {
	cases := map[string]string{
		"cpus too high":    "base: x\ncpus: 99\n",
		"cpus zero":        "base: x\ncpus: 0\n",
		"bad network":      "base: x\nnetwork: wifi\n",
		"tiny ram":         "base: x\nram_mib: 4\n",
		"negative timeout": "base: x\ntimeout_seconds: -1\n",
		"empty base":       "base: \"\"\n",
		"unknown key":      "base: x\nwidgets: 3\n",
		"wrong type":       "base: x\ncpus: many\n",
		"unclosed list":    "base: x\npackages: [jq\n",
		"unterminated str": "base: \"oops\ncpus: 2\n",
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
		s, err := Parse(write(t, "base: "+c.base+"\npackages: [jq]\n"))
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
	apt, _ := Parse(write(t, "base: docker.io/library/debian:12\npackages: [jq]\n"))
	if !strings.Contains(apt.Containerfile(), "rm -rf /var/lib/apt/lists") {
		t.Error("apt should clean its lists")
	}
	dnf, _ := Parse(write(t, "base: docker.io/library/fedora:41\npackages: [jq]\n"))
	if !strings.Contains(dnf.Containerfile(), "dnf clean all") {
		t.Error("dnf should clean its cache")
	}
}

// An unrecognised base must fail at parse time rather than generate a
// Containerfile that dies partway through the build.
func TestUnknownBaseNeedsPkgmgr(t *testing.T) {
	if _, err := Parse(write(t, "base: example.com/custom:1\npackages: [jq]\n")); err == nil {
		t.Error("expected an error asking for pkgmgr")
	}
	s, err := Parse(write(t, "base: example.com/custom:1\npkgmgr: apt\npackages: [jq]\n"))
	if err != nil {
		t.Fatalf("pkgmgr override should work: %v", err)
	}
	if !strings.Contains(s.Containerfile(), "apt-get install") {
		t.Errorf("pkgmgr override ignored:\n%s", s.Containerfile())
	}
	// No packages means no package manager is needed at all.
	if _, err := Parse(write(t, "base: example.com/custom:1\n")); err != nil {
		t.Errorf("base with no packages should parse: %v", err)
	}
	if _, err := Parse(write(t, "base: alpine\npkgmgr: yum\npackages: [jq]\n")); err == nil {
		t.Error("expected an error for an unknown pkgmgr")
	}
}

func TestContainerfileStructure(t *testing.T) {
	s, _ := Parse(write(t, "base: b\nenv:\n  K: V\nrun:\n  - echo hi\n"))
	cf := s.Containerfile()
	if !strings.HasPrefix(cf, "# generated") {
		t.Error("missing generated header")
	}
	if !strings.Contains(cf, "FROM b") || !strings.Contains(cf, "ENV K=V") ||
		!strings.Contains(cf, "RUN echo hi") || !strings.HasSuffix(cf, "WORKDIR /data\n") {
		t.Errorf("structure wrong:\n%s", cf)
	}
}

// Go map iteration is random, so env must be sorted or the generated
// Containerfile changes between runs and busts podman's layer cache.
func TestEnvOrderIsDeterministic(t *testing.T) {
	src := "base: b\nenv:\n  Z: 1\n  A: 2\n  M: 3\n"
	first, _ := Parse(write(t, src))
	want := first.Containerfile()
	for i := 0; i < 20; i++ {
		s, _ := Parse(write(t, src))
		if got := s.Containerfile(); got != want {
			t.Fatalf("output not deterministic:\n%s\nvs\n%s", want, got)
		}
	}
	if !strings.Contains(want, "ENV A=2\nENV M=3\nENV Z=1") {
		t.Errorf("env not sorted:\n%s", want)
	}
}

func TestCommentsAndEmptyFile(t *testing.T) {
	s, err := Parse(write(t, "# just a comment\nbase: b   # trailing\ncpus: 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Base != "b" || s.CPUs != 3 {
		t.Errorf("comment handling wrong: %+v", s)
	}
	// An empty file is valid: every default applies.
	if _, err := Parse(write(t, "")); err != nil {
		t.Errorf("empty file should use defaults: %v", err)
	}
}
