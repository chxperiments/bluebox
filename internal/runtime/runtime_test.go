package runtime

import (
	"strings"
	"testing"

	"bluebox/internal/bluefile"
)

// vmArgs builds every -v in one place, so the declarative mounts from the
// Bluefile must show up there, after /data, with their mode attached.
func TestVmArgsMounts(t *testing.T) {
	t.Setenv("BLUEBOX_HOME", t.TempDir())

	s := bluefile.Default
	s.Mounts = []bluefile.Mount{
		{Host: "/tmp/inputs", Guest: "/inputs", Mode: "ro"},
		{Host: "/tmp/out", Guest: "/out", Mode: "rw"},
		{Host: "/tmp/unset", Guest: "/unset"}, // parse normally defaults this to ro
	}
	args, err := vmArgs("devbox", s, false, true)
	if err != nil {
		t.Fatal(err)
	}
	var mounts []string
	for i, a := range args {
		if a == "-v" && strings.Contains(args[i+1], ":/") && !strings.HasSuffix(args[i+1], ":/data") {
			mounts = append(mounts, args[i+1])
		}
	}
	want := []string{"/tmp/inputs:/inputs:ro", "/tmp/out:/out:rw", "/tmp/unset:/unset:ro"}
	if len(mounts) != len(want) {
		t.Fatalf("mounts wrong: %v", mounts)
	}
	for i, w := range want {
		if mounts[i] != w {
			t.Errorf("mount[%d] = %q, want %q", i, mounts[i], w)
		}
	}
}

func TestVmArgsWithoutMounts(t *testing.T) {
	t.Setenv("BLUEBOX_HOME", t.TempDir())
	args, err := vmArgs("devbox", bluefile.Default, false, true)
	if err != nil {
		t.Fatal(err)
	}
	var data string
	for i, a := range args {
		if a == "-v" {
			data = args[i+1]
		}
	}
	if !strings.HasSuffix(data, ":/data") {
		t.Errorf("expected exactly the /data mount, got %q", data)
	}
}

// Every VM, krun or the plain-container baseline, starts confined: libkrun
// gives the guest whatever its VMM can do, so these flags are the boundary
// behind a libkrun escape and must never silently drop out of vmArgs.
func TestVmArgsConfineTheVMM(t *testing.T) {
	t.Setenv("BLUEBOX_HOME", t.TempDir())
	s := bluefile.Default
	s.RAMMiB = 1024
	for _, krun := range []bool{true, false} {
		args, err := vmArgs("devbox", s, false, krun)
		if err != nil {
			t.Fatal(err)
		}
		joined := " " + strings.Join(args, " ") + " "
		for _, want := range []string{
			" no-new-privileges ",
			" --cap-drop=all ",
			" --cap-add=CHOWN,DAC_OVERRIDE,FOWNER,SETUID,SETGID,NET_BIND_SERVICE ",
			" --pids-limit=512 ",
			" --memory=1280m ",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("krun=%v: missing %q in %v", krun, strings.TrimSpace(want), args)
			}
		}
		if strings.Contains(joined, "--privileged") {
			t.Errorf("krun=%v: --privileged must never be passed", krun)
		}
	}
}
