// Package sandbox owns the on-disk layout of bluebox sandboxes.
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

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
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "sandboxes", name), nil
}

// DataDir is the only path that survives between runs; mounted at /data.
func DataDir(name string) (string, error) {
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
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "logs", name+".log"), nil
}

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

// Snapshot archives a sandbox's /data and returns the archive path. tar is used
// rather than a Go implementation so permissions and symlinks are preserved
// exactly as the guest wrote them.
func Snapshot(name, stamp string) (string, error) {
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
	out := filepath.Join(dir, stamp+".tar.gz")
	cmd := exec.Command("tar", "-czf", out, "-C", data, ".")
	if msg, err := cmd.CombinedOutput(); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("tar: %s", strings.TrimSpace(string(msg)))
	}
	return out, nil
}

// Snapshots lists a sandbox's archives, newest last.
func Snapshots(name string) ([]string, error) {
	dir, err := SnapshotsDir(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // no snapshots yet is not an error
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tar.gz") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
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
