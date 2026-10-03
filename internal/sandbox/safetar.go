package sandbox

import (
	"archive/tar"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// WriteTar streams dir as a tar archive. It never follows a symlink: links
// are archived as links, so a source tree cannot pull in files from outside
// itself. Only directories, regular files and symlinks are written.
func WriteTar(w io.Writer, dir string) error {
	tw := tar.NewWriter(w)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		fi, err := os.Lstat(path)
		if err != nil {
			return err
		}
		link := ""
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		case fi.IsDir(), fi.Mode().IsRegular():
		default:
			return nil // devices, fifos, sockets do not travel
		}
		h, err := tar.FileInfoHeader(fi, link)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		if fi.IsDir() {
			h.Name += "/"
		}
		h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "", ""
		h.Mode &^= int64(fs.ModeSetuid | fs.ModeSetgid)
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if fi.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// SafeUntar extracts an archive the guest produced into dest. The archive is
// untrusted: every operation goes through an os.Root, so neither an entry
// like ../../x nor a symlink planted by an earlier entry can place a file
// outside dest. Devices, fifos and sockets are skipped, setuid and setgid
// bits are dropped, and files belong to whoever extracts them. An entry that
// cannot be placed inside dest is skipped, not fatal, so one hostile entry
// cannot hold back the rest. It returns how many entries it skipped.
func SafeUntar(r io.Reader, dest string) (skipped int, err error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return skipped, nil
		}
		if err != nil {
			return skipped, err
		}
		rel := strings.TrimPrefix(filepath.Clean("/"+h.Name), "/")
		if rel == "" {
			continue
		}
		if err := extractEntry(root, tr, h, rel); err != nil {
			skipped++
		}
	}
}

var errSkip = errors.New("entry type not extracted")

// extractEntry places one entry. Any error, including an os.Root refusal for
// a path that would leave dest, means the entry was not extracted.
func extractEntry(root *os.Root, tr *tar.Reader, h *tar.Header, rel string) error {
	if dir := filepath.Dir(rel); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	perm := fs.FileMode(h.Mode) & fs.ModePerm
	switch h.Typeflag {
	case tar.TypeDir:
		if err := root.Mkdir(rel, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		// Keep it enterable for us whatever the guest set.
		return root.Chmod(rel, perm|0o700)
	case tar.TypeReg:
		if fi, err := root.Lstat(rel); err == nil && !fi.Mode().IsRegular() {
			if err := root.RemoveAll(rel); err != nil {
				return err
			}
		}
		f, err := root.OpenFile(rel, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, tr)
		f.Close()
		if err != nil {
			return err
		}
		return root.Chmod(rel, perm)
	case tar.TypeSymlink:
		if err := root.RemoveAll(rel); err != nil {
			return err
		}
		return root.Symlink(h.Linkname, rel)
	default:
		return errSkip // hard links, devices, fifos
	}
}
