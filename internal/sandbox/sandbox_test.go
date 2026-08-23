package sandbox

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestValidName(t *testing.T) {
	for _, name := range []string{"devbox", "a", "x9", "a-b_c.d"} {
		if err := ValidName(name); err != nil {
			t.Errorf("ValidName(%q) should accept: %v", name, err)
		}
	}
	for _, name := range []string{
		"", ".", "..", "...", "-x", ".hidden",
		"a/b", "../esc", "../../escape", "x/../y",
		"a b", "a\tb", "a\nb",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // 65 chars
	} {
		if err := ValidName(name); err == nil {
			t.Errorf("ValidName(%q) should reject", name)
		}
	}
}

// Every filesystem sink derives its path through these builders, so a
// rejected name makes deletion, moving and creation outside the bluebox
// root unreachable.
func TestPathBuildersRejectUnsafeNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep the assertions independent of the real home
	for _, name := range []string{"../esc", "a/b", "..", ""} {
		if p, err := Dir(name); err == nil {
			t.Errorf("Dir(%q) = %q, want error", name, p)
		}
		if p, err := DataDir(name); err == nil {
			t.Errorf("DataDir(%q) = %q, want error", name, p)
		}
		if p, err := LogPath(name); err == nil {
			t.Errorf("LogPath(%q) = %q, want error", name, p)
		}
		if p, err := SnapshotsDir(name); err == nil {
			t.Errorf("SnapshotsDir(%q) = %q, want error", name, p)
		}
	}
}

func TestPathBuildersAcceptSafeNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := Dir("devbox")
	if err != nil || d == "" {
		t.Errorf("Dir(devbox): %q %v", d, err)
	}
}

// writeArchive builds a .tar.gz containing exactly the named entries. Written
// with archive/tar rather than the tar command so a hostile entry can be
// crafted portably, whichever tar the host ships.
func writeArchive(t *testing.T, path string, members ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, m := range members {
		body := []byte("payload")
		if err := tw.WriteHeader(&tar.Header{
			Name: m, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckMember(t *testing.T) {
	for _, m := range []string{"./", ".", "file.txt", "./sub/nested.txt", "a.b-c_d/e"} {
		if err := checkMember(m); err != nil {
			t.Errorf("checkMember(%q) should accept: %v", m, err)
		}
	}
	for _, m := range []string{
		"/etc/passwd", "/", "../escaped", "../../escaped",
		"./../escaped", "sub/../../escaped", "a/b/../../../c",
	} {
		if err := checkMember(m); err == nil {
			t.Errorf("checkMember(%q) should reject", m)
		}
	}
}

// setup makes a sandbox with data in it and returns its data directory.
func setup(t *testing.T, name string) string {
	t.Helper()
	t.Setenv("BLUEBOX_HOME", t.TempDir())
	if _, err := Create(name); err != nil {
		t.Fatal(err)
	}
	data, err := DataDir(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(data, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, body := range map[string]string{
		"keep.txt":     "original",
		"sub/deep.txt": "nested",
	} {
		if err := os.WriteFile(filepath.Join(data, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return data
}

// A restore returns /data to exactly the snapshot's contents: files changed
// after the snapshot revert, and files added after it are gone.
func TestRestoreRoundTrip(t *testing.T) {
	data := setup(t, "demo")
	archive, err := Snapshot("demo", "20260101T000000Z")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(data, "keep.txt"), []byte("changed"), 0o644)
	os.WriteFile(filepath.Join(data, "added.txt"), []byte("new"), 0o644)

	if err := Restore("demo", archive); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(data, "keep.txt")); string(b) != "original" {
		t.Errorf("keep.txt = %q, want %q", b, "original")
	}
	if b, _ := os.ReadFile(filepath.Join(data, "sub", "deep.txt")); string(b) != "nested" {
		t.Errorf("nested file lost: %q", b)
	}
	if _, err := os.Stat(filepath.Join(data, "added.txt")); !os.IsNotExist(err) {
		t.Error("a file added after the snapshot should not survive a restore")
	}
	// The staging directories must not be left behind.
	for _, leftover := range []string{data + ".restoring", data + ".replaced"} {
		if _, err := os.Stat(leftover); !os.IsNotExist(err) {
			t.Errorf("%s should not survive a restore", filepath.Base(leftover))
		}
	}
}

// An archive whose entries point outside the extraction directory is refused
// before anything is unpacked, and /data is left exactly as it was.
func TestRestoreRefusesTraversalArchive(t *testing.T) {
	data := setup(t, "demo")
	evil := filepath.Join(t.TempDir(), "evil.tar.gz")
	writeArchive(t, evil, "../../escaped.txt")

	if err := Restore("demo", evil); err == nil {
		t.Fatal("a traversing archive must be refused")
	}
	if b, _ := os.ReadFile(filepath.Join(data, "keep.txt")); string(b) != "original" {
		t.Errorf("/data was disturbed by a refused restore: %q", b)
	}
	outside := filepath.Join(filepath.Dir(filepath.Dir(data)), "escaped.txt")
	if _, err := os.Stat(outside); err == nil {
		t.Error("the archive escaped the data directory")
	}
	if _, err := os.Stat(data + ".restoring"); !os.IsNotExist(err) {
		t.Error("a refused restore should not leave a staging directory")
	}
}

func TestRestoreRefusesAbsoluteMember(t *testing.T) {
	setup(t, "demo")
	evil := filepath.Join(t.TempDir(), "abs.tar.gz")
	writeArchive(t, evil, "/etc/passwd")
	if err := Restore("demo", evil); err == nil {
		t.Error("an absolute archive entry must be refused")
	}
}

// A corrupt archive must fail before /data is touched.
func TestRestoreKeepsDataWhenArchiveIsUnreadable(t *testing.T) {
	data := setup(t, "demo")
	bad := filepath.Join(t.TempDir(), "bad.tar.gz")
	os.WriteFile(bad, []byte("not a gzip stream"), 0o644)

	if err := Restore("demo", bad); err == nil {
		t.Fatal("an unreadable archive must be refused")
	}
	if b, _ := os.ReadFile(filepath.Join(data, "keep.txt")); string(b) != "original" {
		t.Errorf("/data was disturbed by a failed restore: %q", b)
	}
}

func TestSnapshotPathResolution(t *testing.T) {
	setup(t, "demo")
	first, err := Snapshot("demo", "20260101T000000Z")
	if err != nil {
		t.Fatal(err)
	}
	latest, err := Snapshot("demo", "20260202T000000Z")
	if err != nil {
		t.Fatal(err)
	}
	// No reference means the most recent snapshot.
	if got, err := SnapshotPath("demo", ""); err != nil || got != latest {
		t.Errorf("SnapshotPath(\"\") = %q %v, want %q", got, err, latest)
	}
	// A bare stamp resolves, with or without the suffix.
	for _, ref := range []string{"20260101T000000Z", "20260101T000000Z.tar.gz"} {
		if got, err := SnapshotPath("demo", ref); err != nil || got != first {
			t.Errorf("SnapshotPath(%q) = %q %v, want %q", ref, got, err, first)
		}
	}
	if _, err := SnapshotPath("demo", "nosuchstamp"); err == nil {
		t.Error("an unknown snapshot name must be an error")
	}
}

func TestSnapshotPathWithoutAnySnapshots(t *testing.T) {
	setup(t, "demo")
	if _, err := SnapshotPath("demo", ""); err == nil {
		t.Error("expected an error when the sandbox has no snapshots")
	}
}
