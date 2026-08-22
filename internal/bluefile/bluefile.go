// Package bluefile parses a Bluefile, the single declarative spec for a
// sandbox, and generates the Containerfile podman builds from.
//
// A Bluefile is YAML describing both the VM (cpus, ram, network, restrictions)
// and its image (base, packages, run steps), so a user never writes a
// Containerfile by hand:
//
//	base: docker.io/library/debian:bookworm-slim
//	cpus: 4
//	ram_mib: 4096
//	network: bridge
//	readonly: true
//	timeout_seconds: 300
//	packages:
//	  - python3
//	  - git
//	run:
//	  - pip3 install --break-system-packages requests
//	env:
//	  LANG: C.UTF-8
package bluefile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Spec is the parsed, validated contents of a Bluefile.
type Spec struct {
	Base           string            `yaml:"base"`
	CPUs           int               `yaml:"cpus"`
	RAMMiB         int               `yaml:"ram_mib"`
	Network        string            `yaml:"network"` // "bridge" (egress) or "none"
	ReadOnlyRootfs bool              `yaml:"readonly"`
	TimeoutSeconds int               `yaml:"timeout_seconds"` // 0 = unlimited
	Seccomp        string            `yaml:"seccomp"`         // profile path, filters the VMM
	PkgMgr         string            `yaml:"pkgmgr"`          // apk/apt/dnf; empty = infer from base
	Packages       []string          `yaml:"packages"`
	Run            []string          `yaml:"run"` // extra Containerfile RUN steps, in order
	Env            map[string]string `yaml:"env"`
}

// Default supplies values a Bluefile omits.
var Default = Spec{
	Base:    "docker.io/library/alpine:latest",
	CPUs:    2,
	RAMMiB:  2048,
	Network: "bridge",
}

// Template is written by `bluebox new`.
const Template = `# Bluefile -- the whole sandbox in one place. bluebox generates the
# Containerfile from this; you never edit that directly.

base: docker.io/library/alpine:latest

cpus: 2
ram_mib: 2048
network: bridge       # bridge = internet access, none = offline
readonly: false       # read-only guest root (/tmp and /data stay writable)
timeout_seconds: 0    # per-run wall clock limit, 0 = unlimited

# Tools baked into the image. Reset every run; keep work in /data.
packages:
  - bash
  - curl
  - git

# Extra build steps, run in order:
# run:
#   - pip3 install --break-system-packages requests

# env:
#   LANG: C.UTF-8
`

// Parse reads and validates a Bluefile. Unknown keys are rejected so a typo
// fails loudly instead of being silently ignored.
func Parse(path string) (Spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, err
	}
	s := Default
	// Decoding empty input clears the destination, wiping the defaults, so
	// skip it entirely: a Bluefile with no content means "all defaults".
	if len(bytes.TrimSpace(b)) > 0 {
		dec := yaml.NewDecoder(bytes.NewReader(b), yaml.DisallowUnknownField())
		if err := dec.Decode(&s); err != nil && !errors.Is(err, io.EOF) {
			return Spec{}, fmt.Errorf("%s: %w", path, err)
		}
	}
	if err := s.validate(); err != nil {
		return Spec{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

func (s Spec) validate() error {
	if s.Base == "" {
		return errors.New("base is required")
	}
	if s.CPUs < 1 || s.CPUs > 16 {
		return fmt.Errorf("cpus must be 1-16 (krun's limit), got %d", s.CPUs)
	}
	if s.RAMMiB < 128 {
		return fmt.Errorf("ram_mib must be at least 128, got %d", s.RAMMiB)
	}
	if s.Network != "bridge" && s.Network != "none" {
		return fmt.Errorf("network must be bridge or none, got %q", s.Network)
	}
	if s.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must be >= 0, got %d", s.TimeoutSeconds)
	}
	if s.Seccomp != "" {
		if _, err := os.Stat(s.Seccomp); err != nil {
			return fmt.Errorf("seccomp profile: %w", err)
		}
	}
	if s.PkgMgr != "" && !isKnownMgr(s.PkgMgr) {
		return fmt.Errorf("pkgmgr must be apk, apt or dnf, got %q", s.PkgMgr)
	}
	if len(s.Packages) > 0 {
		if _, ok := s.pkgMgr(); !ok {
			return fmt.Errorf("cannot infer a package manager for base %q; "+
				"set pkgmgr to apk, apt or dnf", s.Base)
		}
	}
	return nil
}

// pkgMgr returns the package manager for this spec: pkgmgr if set, otherwise
// inferred from the base image name. ok is false when neither works, so the
// caller can ask for pkgmgr instead of emitting a Containerfile that fails
// halfway through a build.
func (s Spec) pkgMgr() (mgr string, ok bool) {
	if s.PkgMgr != "" {
		return strings.ToLower(s.PkgMgr), isKnownMgr(s.PkgMgr)
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
	switch strings.ToLower(m) {
	case "apk", "apt", "dnf":
		return true
	}
	return false
}

// Containerfile renders the image build instructions for this spec. The package
// manager comes from pkgmgr, or is inferred from the base image name.
func (s Spec) Containerfile() string {
	var b strings.Builder
	b.WriteString("# generated by bluebox from the Bluefile -- do not edit\n")
	fmt.Fprintf(&b, "FROM %s\n\n", s.Base)

	// Sorted so the same Bluefile always yields byte-identical output, which
	// keeps podman's layer cache warm across rebuilds.
	if len(s.Env) > 0 {
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "ENV %s=%s\n", k, s.Env[k])
		}
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
