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
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// Spec is the parsed, validated contents of a Bluefile.
type Spec struct {
	Base           string            `yaml:"base"`
	CPUs           int               `yaml:"cpus"`
	RAMMiB         int               `yaml:"ram_mib"`
	Network        string            `yaml:"network"` // "bridge" (egress) or "none"
	Passt          bool              `yaml:"passt"`   // real in-guest interface + default route
	ReadOnlyRootfs bool              `yaml:"readonly"`
	TimeoutSeconds int               `yaml:"timeout_seconds"` // 0 = unlimited
	Seccomp        string            `yaml:"seccomp"`         // profile path, filters the VMM
	PkgMgr         string            `yaml:"pkgmgr"`          // apk/apt/dnf; empty = infer from base
	Packages       []string          `yaml:"packages"`
	Run            []string          `yaml:"run"` // extra Containerfile RUN steps, in order
	Env            map[string]string `yaml:"env"`
	Blueprint      Blueprint         `yaml:"blueprint"`
}

// Blueprint is cloud-init-style provisioning. Unlike cloud-init it is applied
// at build time, not first boot: every run is a fresh VM, so provisioning at
// boot would repeat on every command.
type Blueprint struct {
	Users      []User      `yaml:"users"`
	WriteFiles []WriteFile `yaml:"write_files"`
	RunCmd     []string    `yaml:"runcmd"`
}

type User struct {
	Name  string `yaml:"name"`
	Shell string `yaml:"shell"` // defaults to /bin/sh
	Sudo  bool   `yaml:"sudo"`  // passwordless sudo; needs sudo in packages
}

type WriteFile struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
	Mode    string `yaml:"mode"` // e.g. "0755"; optional
}

func (b Blueprint) empty() bool {
	return len(b.Users) == 0 && len(b.WriteFiles) == 0 && len(b.RunCmd) == 0
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
passt: false          # real NIC + default route in the guest (needed for k3s)
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

// Grammar rules for spec fields that are data, not instructions. A value
// violating its rule could otherwise change the structure of the generated
// Containerfile (a newline in an env value becomes a new instruction, a mode
// like "0755; x" becomes a second shell command), so it is rejected at parse
// time instead of being escaped at render time.
var (
	envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	modeRe   = regexp.MustCompile(`^[0-7]{3,4}$`)
	userRe   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	shellRe  = regexp.MustCompile(`^/[A-Za-z0-9/._-]+$`)
	pathRe   = regexp.MustCompile(`^/[A-Za-z0-9/._-]+$`)
)

func (s Spec) validate() error {
	if s.Base == "" {
		return errors.New("base is required")
	}
	if strings.ContainsAny(s.Base, " \t\r\n\x00") {
		return fmt.Errorf("base must be an image reference without whitespace or control characters, got %q", s.Base)
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
	// A blueprint needs the package manager too, for the right useradd form.
	if len(s.Packages) > 0 || len(s.Blueprint.Users) > 0 {
		if _, ok := s.pkgMgr(); !ok {
			return fmt.Errorf("cannot infer a package manager for base %q; "+
				"set pkgmgr to apk, apt or dnf", s.Base)
		}
	}
	for i, u := range s.Blueprint.Users {
		if strings.TrimSpace(u.Name) == "" {
			return fmt.Errorf("blueprint.users[%d]: name is required", i)
		}
		if !userRe.MatchString(u.Name) {
			return fmt.Errorf("blueprint.users[%d]: name must be lowercase letters, digits, '_' or '-' (max 32), got %q", i, u.Name)
		}
		if u.Shell != "" && !shellRe.MatchString(u.Shell) {
			return fmt.Errorf("blueprint.users[%d]: shell must be an absolute path (letters, digits, '/', '.', '_', '-'), got %q", i, u.Shell)
		}
	}
	for i, f := range s.Blueprint.WriteFiles {
		// The path is interpolated into a COPY and, when a mode is set, into a
		// RUN chmod line -- so it is build grammar the same way mode is. A path
		// like "/tmp/x; touch /PWNED" would inject a second shell command.
		if !pathRe.MatchString(f.Path) {
			return fmt.Errorf("blueprint.write_files[%d]: path must be an absolute path (letters, digits, '/', '.', '_', '-'), got %q", i, f.Path)
		}
		if f.Mode != "" && !modeRe.MatchString(f.Mode) {
			return fmt.Errorf("blueprint.write_files[%d]: mode must be octal (e.g. \"0644\"), got %q", i, f.Mode)
		}
	}
	for _, k := range s.EnvKeys() {
		if !envKeyRe.MatchString(k) {
			return fmt.Errorf("env: key must be an identifier (letters, digits, '_'), got %q", k)
		}
		if strings.ContainsAny(s.Env[k], "\r\n") {
			return fmt.Errorf("env[%s]: value must not contain a newline; put build steps in 'run' instead", k)
		}
	}
	for i, p := range s.Packages {
		if p == "" || strings.ContainsAny(p, " \t\r\n;|&$`") {
			return fmt.Errorf("packages[%d]: %q is not a plain package name", i, p)
		}
	}
	return nil
}

// EnvKeys returns the env keys in sorted order.
func (s Spec) EnvKeys() []string {
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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

// assetDir holds blueprint file contents inside the build context. Contents go
// through COPY rather than being escaped into a RUN, so arbitrary text (quotes,
// newlines, shell metacharacters) survives intact.
const assetDir = ".blueprint"

// Render writes any blueprint assets into the build context at dir and returns
// the Containerfile text. Use it instead of Containerfile when the spec may
// carry a blueprint.
func (s Spec) Render(dir string) (string, error) {
	target := filepath.Join(dir, assetDir)
	// Clear stale assets so a removed write_files entry cannot linger.
	if err := os.RemoveAll(target); err != nil {
		return "", err
	}
	if len(s.Blueprint.WriteFiles) > 0 {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return "", err
		}
		for i, f := range s.Blueprint.WriteFiles {
			p := filepath.Join(target, assetName(i))
			if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
				return "", err
			}
		}
	}
	return s.Containerfile(), nil
}

func assetName(i int) string { return "f" + strconv.Itoa(i) }

// Containerfile renders the image build instructions for this spec. The package
// manager comes from pkgmgr, or is inferred from the base image name. Blueprint
// write_files are referenced by COPY; call Render first to materialise them.
func (s Spec) Containerfile() string {
	var b strings.Builder
	b.WriteString("# generated by bluebox from the Bluefile -- do not edit\n")
	fmt.Fprintf(&b, "FROM %s\n\n", s.Base)

	// Sorted so the same Bluefile always yields byte-identical output, which
	// keeps podman's layer cache warm across rebuilds.
	if len(s.Env) > 0 {
		for _, k := range s.EnvKeys() {
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
	if !s.Blueprint.empty() {
		mgr, _ := s.pkgMgr()
		b.WriteString(s.Blueprint.render(mgr))
	}
	b.WriteString("WORKDIR /data\n")
	return b.String()
}

// render emits the blueprint as Containerfile steps: users, then files, then
// commands -- so a runcmd can rely on both existing.
func (b Blueprint) render(mgr string) string {
	var out strings.Builder
	for _, u := range b.Users {
		shell := u.Shell
		if shell == "" {
			shell = "/bin/sh"
		}
		if mgr == "apk" {
			fmt.Fprintf(&out, "RUN adduser -D -s %s %s\n", shell, u.Name)
		} else {
			fmt.Fprintf(&out, "RUN useradd -m -s %s %s\n", shell, u.Name)
		}
		if u.Sudo {
			// wheel on apk/dnf, sudo on apt; the sudoers drop-in is what
			// actually grants access, the group just matches convention.
			group := "wheel"
			if mgr == "apt" {
				group = "sudo"
			}
			fmt.Fprintf(&out, "RUN usermod -aG %s %s || addgroup %s %s || true\n",
				group, u.Name, u.Name, group)
			fmt.Fprintf(&out, "RUN mkdir -p /etc/sudoers.d && "+
				"echo '%s ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/%s && "+
				"chmod 0440 /etc/sudoers.d/%s\n", u.Name, u.Name, u.Name)
		}
	}
	for i, f := range b.WriteFiles {
		fmt.Fprintf(&out, "COPY %s/%s %s\n", assetDir, assetName(i), f.Path)
		if f.Mode != "" {
			fmt.Fprintf(&out, "RUN chmod %s %s\n", f.Mode, f.Path)
		}
	}
	for _, c := range b.RunCmd {
		fmt.Fprintf(&out, "RUN %s\n", c)
	}
	if out.Len() > 0 {
		out.WriteByte('\n')
	}
	return out.String()
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
