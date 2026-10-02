package sandbox

import (
	"io/fs"
	"os"
	"syscall"
)

// isWhiteout reports an overlayfs deletion marker: a character device 0:0.
func isWhiteout(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && fi.Mode()&fs.ModeCharDevice != 0 && st.Rdev == 0
}

// isOpaque reports an upper directory that replaces, rather than overlays,
// the lower one. Unprivileged overlayfs (what rootless podman mounts) keeps
// the mark in the user namespace: user.overlay.opaque.
func isOpaque(path string) bool {
	var buf [1]byte
	n, err := syscall.Getxattr(path, "user.overlay.opaque", buf[:])
	if err != nil || n < 1 {
		n, err = syscall.Getxattr(path, "trusted.overlay.opaque", buf[:])
	}
	return err == nil && n >= 1 && buf[0] == 'y'
}

// chownLike gives dst/rel the owner of src. Under podman unshare -- where a
// strict sandbox's /data is handled -- IDs are the namespace's, so the guest
// root's files stay the guest root's.
func chownLike(dst *os.Root, rel string, src fs.FileInfo) error {
	st, ok := src.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if err := dst.Lchown(rel, int(st.Uid), int(st.Gid)); err != nil && !os.IsPermission(err) {
		return err
	}
	return nil
}
