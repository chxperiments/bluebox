// Package bluefile parses a Bluefile, the single declarative spec for a
// sandbox, and generates the Containerfile podman builds from.
//
// A Bluefile describes both the VM (cpus, ram, network, restrictions) and its
// image (base, packages, run steps) in one place, so a user never writes a
// Containerfile by hand.
//
// Format is line-oriented `KEY value`, one directive per line. `#` comments and
// blank lines are ignored. Repeatable directives (PACKAGE, RUN, ENV) accumulate.
//
//	BASE       docker.io/library/debian:bookworm-slim
//	CPUS       4
//	RAM        4096
//	NETWORK    bridge
//	READONLY   true
//	TIMEOUT    300
//	SECCOMP    /home/me/.bluebox/seccomp/tight.json
//	PKGMGR     apt
//	PACKAGE    python3 python3-pip git
//	PACKAGE    ripgrep jq
//	RUN        pip3 install --break-system-packages requests
//	ENV        LANG=C.UTF-8
package bluefile

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Spec is the parsed, validated contents of a Bluefile.
type Spec struct {
	Base           string
	CPUs           int
	RAMMiB         int
	Network        string // "bridge" (egress) or "none" (airgapped)
	ReadOnlyRootfs bool
	TimeoutSeconds int
	Seccomp        string
	PkgMgr         string   // "apk"/"apt"/"dnf"; empty means infer from Base
	Packages       []string // installed via the base image's package manager
	Run            []string // extra Containerfile RUN steps, in order
	Env            []string // KEY=VALUE pairs
}

// Default is used for values a Bluefile omits.
var Default = Spec{
	Base:    "docker.io/library/alpine:latest",
	CPUs:    2,
	RAMMiB:  2048,
	Network: "bridge",
}

// Template is written by `bluebox new`.
const Template = `# Bluefile -- the whole sandbox in one place. bluebox generates the
# Containerfile from this; you never edit that directly.

BASE     docker.io/library/alpine:latest

CPUS     2
RAM      2048          # MiB
NETWORK  bridge        # bridge = internet access, none = airgapped
READONLY false         # read-only guest rootfs (/tmp and /data stay writable)
TIMEOUT  0             # seconds per run, 0 = unlimited

# Tools baked into the image (reset every run). Repeat PACKAGE freely.
PACKAGE  bash curl git

# Extra build steps, run in order:
# RUN    pip3 install --break-system-packages requests
`

// Parse reads and validates a Bluefile.
func Parse(path string) (Spec, error) {
	f, err := os.Open(path)
	if err != nil {
		return Spec{}, err
	}
	defer f.Close()

	s := Default
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := stripComment(sc.Text())
		if strings.TrimSpace(text) == "" {
			continue
		}
		key, val, ok := splitDirective(text)
		if !ok {
			return Spec{}, fmt.Errorf("%s:%d: expected `KEY value`, got %q", path, line, sc.Text())
		}
		if err := s.apply(key, val); err != nil {
			return Spec{}, fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
	if err := sc.Err(); err != nil {
		return Spec{}, err
	}
	if err := s.validate(); err != nil {
		return Spec{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

func (s *Spec) apply(key, val string) error {
	switch strings.ToUpper(key) {
	case "BASE":
		s.Base = val
	case "CPUS":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("CPUS: %w", err)
		}
		s.CPUs = n
	case "RAM":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("RAM: %w", err)
		}
		s.RAMMiB = n
	case "NETWORK":
		s.Network = val
	case "READONLY":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("READONLY: want true/false, got %q", val)
		}
		s.ReadOnlyRootfs = b
	case "TIMEOUT":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("TIMEOUT: %w", err)
		}
		s.TimeoutSeconds = n
	case "SECCOMP":
		s.Seccomp = val
	case "PKGMGR":
		s.PkgMgr = strings.ToLower(val)
	case "PACKAGE":
		s.Packages = append(s.Packages, strings.Fields(val)...)
	case "RUN":
		s.Run = append(s.Run, val)
	case "ENV":
		if !strings.Contains(val, "=") {
			return fmt.Errorf("ENV: want KEY=VALUE, got %q", val)
		}
		s.Env = append(s.Env, val)
	default:
		return fmt.Errorf("unknown directive %q", key)
	}
	return nil
}

func (s Spec) validate() error {
	if s.Base == "" {
		return fmt.Errorf("BASE is required")
	}
	if s.CPUs < 1 || s.CPUs > 16 {
		return fmt.Errorf("CPUS must be 1-16 (krun's limit), got %d", s.CPUs)
	}
	if s.RAMMiB < 128 {
		return fmt.Errorf("RAM must be at least 128 MiB, got %d", s.RAMMiB)
	}
	if s.Network != "bridge" && s.Network != "none" {
		return fmt.Errorf("NETWORK must be bridge or none, got %q", s.Network)
	}
	if s.TimeoutSeconds < 0 {
		return fmt.Errorf("TIMEOUT must be >= 0, got %d", s.TimeoutSeconds)
	}
	if s.Seccomp != "" {
		if _, err := os.Stat(s.Seccomp); err != nil {
			return fmt.Errorf("SECCOMP profile: %w", err)
		}
	}
	if s.PkgMgr != "" && !isKnownMgr(s.PkgMgr) {
		return fmt.Errorf("PKGMGR must be apk, apt or dnf, got %q", s.PkgMgr)
	}
	if len(s.Packages) > 0 {
		if _, ok := s.pkgMgr(); !ok {
			return fmt.Errorf("cannot infer a package manager for BASE %q; "+
				"set PKGMGR to apk, apt or dnf", s.Base)
		}
	}
	return nil
}

// Containerfile renders the image build instructions for this spec. The package
// manager comes from PKGMGR, or is inferred from the base image name.
func (s Spec) Containerfile() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# generated by bluebox from the Bluefile -- do not edit\n")
	fmt.Fprintf(&b, "FROM %s\n\n", s.Base)
	for _, e := range s.Env {
		fmt.Fprintf(&b, "ENV %s\n", e)
	}
	if len(s.Env) > 0 {
		b.WriteByte('\n')
	}
	if len(s.Packages) > 0 {
		mgr, _ := s.pkgMgr() // validate() already guaranteed this resolves
		fmt.Fprintf(&b, "RUN %s\n\n", installCmd(mgr, s.Packages))
	}
	for _, r := range s.Run {
		fmt.Fprintf(&b, "RUN %s\n", r)
	}
	if len(s.Run) > 0 {
		b.WriteByte('\n')
	}
	b.WriteString("WORKDIR /data\n")
	return b.String()
}

// pkgMgr returns the package manager for this spec: PKGMGR if set, otherwise
// inferred from the base image name. ok is false when neither works, so the
// caller can ask for PKGMGR instead of emitting a Containerfile that fails
// halfway through a build.
func (s Spec) pkgMgr() (mgr string, ok bool) {
	if s.PkgMgr != "" {
		return s.PkgMgr, isKnownMgr(s.PkgMgr)
	}
	l := strings.ToLower(s.Base)
	for _, d := range []struct{ match, mgr string }{
		{"alpine", "apk"},
		{"debian", "apt"},
		{"ubuntu", "apt"},
		{"fedora", "dnf"},
		{"rockylinux", "dnf"},
		{"almalinux", "dnf"},
		{"centos", "dnf"},
	} {
		if strings.Contains(l, d.match) {
			return d.mgr, true
		}
	}
	return "", false
}

func isKnownMgr(m string) bool {
	return m == "apk" || m == "apt" || m == "dnf"
}

// installCmd builds a non-interactive install line, with the cache cleanup each
// manager needs to avoid bloating the image layer.
func installCmd(mgr string, pkgs []string) string {
	list := strings.Join(pkgs, " ")
	switch mgr {
	case "apt":
		return "apt-get update && apt-get install -y --no-install-recommends " +
			list + " && rm -rf /var/lib/apt/lists/*"
	case "dnf":
		return "dnf install -y " + list + " && dnf clean all"
	default: // apk
		return "apk add --no-cache " + list
	}
}

func stripComment(line string) string {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		return line[:i]
	}
	return line
}

func splitDirective(line string) (key, val string, ok bool) {
	line = strings.TrimSpace(line)
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return "", "", false
	}
	return line[:i], strings.TrimSpace(line[i+1:]), true
}
