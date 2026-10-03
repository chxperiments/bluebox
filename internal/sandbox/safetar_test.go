package sandbox

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTarRoundTrip(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "a"), "A")
	write(t, filepath.Join(src, "d", "b"), "B")
	os.Symlink("a", filepath.Join(src, "rel"))
	os.Chmod(filepath.Join(src, "a"), 0o4750)
	var buf bytes.Buffer
	if err := WriteTar(&buf, src); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if _, err := SafeUntar(&buf, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "d", "b")); string(b) != "B" {
		t.Error("nested file lost")
	}
	if l, _ := os.Readlink(filepath.Join(dst, "rel")); l != "a" {
		t.Error("symlink not kept as a symlink")
	}
	if fi, _ := os.Stat(filepath.Join(dst, "a")); fi.Mode()&os.ModeSetuid != 0 || fi.Mode().Perm() != 0o750 {
		t.Errorf("mode = %v, want 0750 without setuid", fi.Mode())
	}
}

// A guest-made archive must not be able to write outside the destination,
// by path or by a symlink one entry plants for the next to write through.
func TestSafeUntarStaysInside(t *testing.T) {
	outside := t.TempDir()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	add := func(h *tar.Header, body string) {
		h.Size = int64(len(body))
		tw.WriteHeader(h)
		tw.Write([]byte(body))
	}
	add(&tar.Header{Name: "../../escape", Typeflag: tar.TypeReg, Mode: 0o644}, "x")
	add(&tar.Header{Name: "/abs", Typeflag: tar.TypeReg, Mode: 0o644}, "x")
	add(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: outside}, "")
	add(&tar.Header{Name: "link/pwned", Typeflag: tar.TypeReg, Mode: 0o644}, "x")
	add(&tar.Header{Name: "dev", Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3}, "")
	add(&tar.Header{Name: "hard", Typeflag: tar.TypeLink, Linkname: "/etc/passwd"}, "")
	tw.Close()

	dst := t.TempDir()
	skipped, err := SafeUntar(&buf, dst)
	if err != nil {
		t.Fatalf("a hostile entry must be skipped, not abort the rest: %v", err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("wrote outside the destination: %v", entries)
	}
	// link/pwned (through the planted symlink), the device and the hard link.
	if skipped != 3 {
		t.Errorf("skipped = %d, want 3", skipped)
	}
	// Absolute and ../ names are confined to dst, not dropped.
	for _, p := range []string{"escape", "abs"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("%s should land inside dst: %v", p, err)
		}
	}
}
