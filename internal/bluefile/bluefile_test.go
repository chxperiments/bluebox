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
PACKAGE  nmap python3
PACKAGE  gdb
RUN      pip install pwntools
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
	if got := strings.Join(s.Packages, ","); got != "nmap,python3,gdb" {
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

func TestContainerfileApkVsApt(t *testing.T) {
	apk, _ := Parse(write(t, "BASE docker.io/library/alpine:latest\nPACKAGE nmap\n"))
	if !strings.Contains(apk.Containerfile(), "apk add --no-cache nmap") {
		t.Errorf("alpine should use apk:\n%s", apk.Containerfile())
	}
	apt, _ := Parse(write(t, "BASE docker.io/kalilinux/kali-rolling\nPACKAGE nmap\n"))
	cf := apt.Containerfile()
	if !strings.Contains(cf, "apt-get install") || !strings.Contains(cf, "rm -rf /var/lib/apt/lists") {
		t.Errorf("kali should use apt with cleanup:\n%s", cf)
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
