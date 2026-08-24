// Package sandbox owns the on-disk layout of bluebox sandboxes.
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// nameRe keeps a sandbox name a single safe path component, so every path
// derived from a name stays inside the bluebox root. The alphanumeric first
// character rejects ".", ".." and hidden names in one stroke.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidName reports whether name is usable as a sandbox name.
func ValidName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid sandbox name %q: use letters, digits, '.', '_' or '-' (max 64, starting with a letter or digit)", name)
	}
	return nil
}

// ValidLabel reports whether label is usable to name a snapshot. Labels share
// the sandbox name grammar: an archive is <label>.tar.gz, so a label has to be
// a single safe path component for the same reason a name does.
func ValidLabel(label string) error {
	if !nameRe.MatchString(label) {
		return fmt.Errorf("invalid snapshot name %q: use letters, digits, '.', '_' or '-' (max 64, starting with a letter or digit)", label)
	}
	return nil
}

// Home is the bluebox root, overridable with BLUEBOX_HOME.
func Home() (string, error) {
	if h := os.Getenv("BLUEBOX_HOME"); h != "" {
		return h, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(h, ".bluebox"), nil
}

// Dir holds a sandbox's definition (its Bluefile and generated Containerfile).
func Dir(name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "sandboxes", name), nil
}

// DataDir is the only path that survives between runs; mounted at /data.
func DataDir(name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "data", name), nil
}

func BluefilePath(name string) (string, error) {
	d, err := Dir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "Bluefile"), nil
}

// ContainerfilePath is a build artifact generated from the Bluefile.
func ContainerfilePath(name string) (string, error) {
	d, err := Dir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "Containerfile"), nil
}

// LogPath is the append-only record of runs for a sandbox.
func LogPath(name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "logs", name+".log"), nil
}

// ImageTag formats the podman image tag. The name is not re-validated here:
// every call site reaches this through a path builder that already ran
// ValidName, and podman itself rejects malformed refs loudly.
func ImageTag(name string) string { return "bluebox/" + name + ":latest" }

// VerifyCachePath holds the last successful isolation check, keyed on the
// runtime's identity. It is not per-sandbox: isolation is a property of the
// podman + krun toolchain, so one cleared runtime clears it for every sandbox.
func VerifyCachePath() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "verify.json"), nil
}

// SnapshotsDir holds archived copies of a sandbox's /data.
func SnapshotsDir(name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "snapshots", name), nil
}

// DataEmpty reports whether a sandbox has nothing worth losing.
func DataEmpty(name string) (bool, error) {
	d, err := DataDir(name)
	if err != nil {
		return false, err
	}
	entries, err := os.ReadDir(d)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// ResetData empties a sandbox's /data, leaving the sandbox itself intact.
func ResetData(name string) error {
	d, err := DataDir(name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(d); err != nil {
		return err
	}
	return os.MkdirAll(d, 0o755)
}

// Snapshot archives a sandbox's /data as label and returns the archive path.
// The label is either a timestamp or a name the user chose; either way it
// becomes a filename, so it is validated here rather than trusted. tar is used
// rather than a Go implementation so permissions and symlinks are preserved
// exactly as the guest wrote them.
func Snapshot(name, label string) (string, error) {
	if err := ValidLabel(label); err != nil {
		return "", err
	}
	data, err := DataDir(name)
	if err != nil {
		return "", err
	}
	dir, err := SnapshotsDir(name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(dir, label+".tar.gz")
	// Written beside the target and renamed into place, so an interrupted
	// snapshot leaves no half-written archive -- and, when a label is being
	// reused, does not destroy the archive it was going to replace.
	tmp := out + ".partial"
	cmd := exec.Command("tar", "-czf", tmp, "-C", data, ".")
	if msg, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("tar: %s", strings.TrimSpace(string(msg)))
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return out, nil
}

// Snapshots lists a sandbox's archives, newest last. They are ordered by
// modification time rather than by name: a labelled snapshot sorts nowhere
// near a timestamped one, so a name says nothing about which came last.
func Snapshots(name string) ([]string, error) {
	dir, err := SnapshotsDir(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // no snapshots yet is not an error
	}
	type archive struct {
		path string
		mod  time.Time
	}
	var found []archive
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // vanished between the read and the stat
		}
		found = append(found, archive{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].mod.Equal(found[j].mod) {
			return found[i].path < found[j].path // stable for same-second writes
		}
		return found[i].mod.Before(found[j].mod)
	})
	out := make([]string, 0, len(found))
	for _, a := range found {
		out = append(out, a.path)
	}
	return out, nil
}

// SnapshotPath resolves a snapshot reference to an archive path. A bare
// reference names a snapshot of this sandbox (with or without the .tar.gz
// suffix); an empty one means the most recent. A reference containing a
// separator is taken as a path to an archive kept elsewhere.
func SnapshotPath(name, ref string) (string, error) {
	if ref == "" {
		snaps, err := Snapshots(name)
		if err != nil {
			return "", err
		}
		if len(snaps) == 0 {
			return "", fmt.Errorf("no snapshots for %q; take one with: bluebox snapshot %s", name, name)
		}
		return snaps[len(snaps)-1], nil // Snapshots sorts newest last
	}
	if !strings.ContainsRune(ref, filepath.Separator) {
		dir, err := SnapshotsDir(name)
		if err != nil {
			return "", err
		}
		for _, cand := range []string{filepath.Join(dir, ref+".tar.gz"), filepath.Join(dir, ref)} {
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand, nil
			}
		}
		return "", fmt.Errorf("no snapshot %q for %s; list them with: bluebox snapshot %s -l", ref, name, name)
	}
	if fi, err := os.Stat(ref); err != nil || fi.IsDir() {
		return "", fmt.Errorf("no archive at %s", ref)
	}
	return ref, nil
}

// checkMember rejects an archive entry that would write outside the directory
// it is extracted into. GNU tar refuses these itself, but bsdtar (the tar on
// macOS) differs, so the check is made here rather than assumed of the tool.
func checkMember(m string) error {
	// Checked before the trailing slash is trimmed, or a bare "/" would look
	// like the archive root rather than an absolute path.
	if strings.HasPrefix(m, "/") || filepath.IsAbs(m) {
		return fmt.Errorf("entry %q is an absolute path", m)
	}
	p := strings.TrimSuffix(m, "/")
	if p == "" || p == "." {
		return nil // the archive root
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return fmt.Errorf("entry %q escapes the archive root", m)
		}
	}
	if c := filepath.Clean(p); c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
		return fmt.Errorf("entry %q escapes the archive root", m)
	}
	return nil
}

// VerifyArchive reads an archive's index and fails if any entry would land
// outside the extraction directory.
func VerifyArchive(archive string) error {
	out, err := exec.Command("tar", "-tzf", archive).Output()
	if err != nil {
		return fmt.Errorf("cannot read %s as a gzip archive", filepath.Base(archive))
	}
	for _, m := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if m == "" {
			continue
		}
		if err := checkMember(m); err != nil {
			return err
		}
	}
	return nil
}

// Restore replaces a sandbox's /data with the contents of an archive. The
// archive is checked first, then unpacked beside the existing data and swapped
// in only once it is complete, so a failed restore leaves /data as it was.
func Restore(name, archive string) error {
	data, err := DataDir(name)
	if err != nil {
		return err
	}
	if err := VerifyArchive(archive); err != nil {
		return err
	}
	staged, replaced := data+".restoring", data+".replaced"
	os.RemoveAll(staged)
	os.RemoveAll(replaced)
	if err := os.MkdirAll(staged, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("tar", "-xzf", archive, "-C", staged)
	if msg, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(staged)
		return fmt.Errorf("tar: %s", strings.TrimSpace(string(msg)))
	}
	if _, err := os.Stat(data); err == nil {
		if err := os.Rename(data, replaced); err != nil {
			os.RemoveAll(staged)
			return err
		}
	}
	if err := os.Rename(staged, data); err != nil {
		os.Rename(replaced, data) // put the original back
		os.RemoveAll(staged)
		return err
	}
	os.RemoveAll(replaced)
	return nil
}

// Remove deletes a sandbox definition. Data is kept unless withData is set,
// because it is the only thing a rebuild cannot reproduce.
func Remove(name string, withData bool) error {
	dir, err := Dir(name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if log, err := LogPath(name); err == nil {
		os.Remove(log)
	}
	if withData {
		data, err := DataDir(name)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(data); err != nil {
			return err
		}
		// Snapshots are copies of that same data, so they go with it.
		if snaps, err := SnapshotsDir(name); err == nil {
			return os.RemoveAll(snaps)
		}
	}
	return nil
}

// Rename moves a sandbox's definition, data and log to a new name.
func Rename(from, to string) error {
	if !Exists(from) {
		return fmt.Errorf("no sandbox %q", from)
	}
	if Exists(to) {
		return fmt.Errorf("sandbox %q already exists", to)
	}
	type pair struct{ src, dst func(string) (string, error) }
	for _, p := range []pair{{Dir, Dir}, {DataDir, DataDir}, {LogPath, LogPath}} {
		src, err := p.src(from)
		if err != nil {
			return err
		}
		dst, err := p.dst(to)
		if err != nil {
			return err
		}
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue // data or log may not exist yet
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	return nil
}

// Exists reports whether a sandbox has been created.
func Exists(name string) bool {
	p, err := BluefilePath(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Create makes the directories for a new sandbox.
func Create(name string) (dir string, err error) {
	dir, err = Dir(name)
	if err != nil {
		return "", err
	}
	if Exists(name) {
		return "", fmt.Errorf("sandbox %q already exists at %s", name, dir)
	}
	data, err := DataDir(name)
	if err != nil {
		return "", err
	}
	for _, d := range []string{dir, data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// List returns the names of all defined sandboxes.
func List() ([]string, error) {
	h, err := Home()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(h, "sandboxes"))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && Exists(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
