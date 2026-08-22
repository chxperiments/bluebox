// Package sandbox owns the on-disk layout of bluebox sandboxes.
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

func ImageTag(name string) string { return "bluebox/" + name + ":latest" }

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
