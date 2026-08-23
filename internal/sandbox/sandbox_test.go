package sandbox

import "testing"

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
