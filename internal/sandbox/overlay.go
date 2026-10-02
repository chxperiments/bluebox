package sandbox

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Change is one entry of a fork's upper layer, relative to /data.
type Change struct {
	Path string
	Kind ChangeKind
	Mode fs.FileMode // of the new entry; zero for a deletion
}

type ChangeKind byte

const (
	Added    ChangeKind = 'A'
	Modified ChangeKind = 'M'
	Deleted  ChangeKind = 'D'
)

func (k ChangeKind) String() string { return string(k) }

// Diff lists what a fork has changed relative to its parent, from the
// overlay upper layer alone, so it is exact and costs no VM. overlayfs
// records a deletion as a whiteout (a 0:0 character device) and a replaced
// directory as an opaque one, whose lower contents are hidden: those lower
// entries are reported as deleted too.
func Diff(name string) ([]Change, error) {
	parent, err := Parent(name)
	if err != nil {
		return nil, err
	}
	upper, err := UpperDir(name)
	if err != nil {
		return nil, err
	}
	lowerPath, err := DataDir(parent)
	if err != nil {
		return nil, err
	}
	// Existence checks in the lower layer go through a Root, so a symlink the
	// parent's sandbox planted there cannot send them elsewhere.
	lower, err := os.OpenRoot(lowerPath)
	if err != nil {
		if os.IsNotExist(err) {
			lower = nil // a parent that never ran has no data yet
		} else {
			return nil, err
		}
	}
	if lower != nil {
		defer lower.Close()
	}
	inLower := func(rel string) (fs.FileInfo, bool) {
		if lower == nil {
			return nil, false
		}
		fi, err := lower.Lstat(rel)
		return fi, err == nil
	}

	var out []Change
	var walk func(rel string, hidden bool) error
	walk = func(rel string, hidden bool) error {
		entries, err := os.ReadDir(filepath.Join(upper, rel))
		if err != nil {
			return err
		}
		for _, e := range entries {
			erel := filepath.Join(rel, e.Name())
			fi, err := e.Info()
			if err != nil {
				return err
			}
			if isWhiteout(fi) {
				out = append(out, Change{Path: erel, Kind: Deleted})
				continue
			}
			lfi, existed := inLower(erel)
			kind := Added
			if existed && !hidden {
				kind = Modified
			}
			if fi.IsDir() {
				opaque := isOpaque(filepath.Join(upper, erel))
				if existed && lfi.IsDir() && !hidden && !opaque {
					// An ordinary upper directory only carries its children;
					// the directory itself is unchanged.
					if err := walk(erel, false); err != nil {
						return err
					}
					continue
				}
				out = append(out, Change{Path: erel, Kind: kind, Mode: fi.Mode()})
				if opaque && existed && lfi.IsDir() {
					// Replaced wholesale: whatever the lower held under it and
					// the upper does not is gone.
					hiddenEntries, _ := lowerOnly(lower, erel, filepath.Join(upper, erel))
					for _, h := range hiddenEntries {
						out = append(out, Change{Path: filepath.Join(erel, h), Kind: Deleted})
					}
				}
				if err := walk(erel, hidden || opaque || !(existed && lfi.IsDir())); err != nil {
					return err
				}
				continue
			}
			out = append(out, Change{Path: erel, Kind: kind, Mode: fi.Mode()})
		}
		return nil
	}
	if err := walk(".", false); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// lowerOnly names the entries of lower/rel that upperDir does not contain.
func lowerOnly(lower *os.Root, rel, upperDir string) ([]string, error) {
	f, err := lower.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range names {
		if _, err := os.Lstat(filepath.Join(upperDir, n)); err != nil {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Apply merges a fork's changes into its parent's /data and then clears the
// fork, so it carries on from the merged state.
//
// The parent's /data is guest-written and so untrusted: it may hold a
// symlink to anywhere on the host, placed exactly where the fork then wrote
// a file. Every destination operation therefore goes through an os.Root,
// which refuses to follow a symlink out of /data, and an existing entry
// that is not what the upper layer has there is removed rather than written
// through. Entry types that cannot be files in /data -- devices, fifos,
// sockets -- are skipped and reported; setuid and setgid bits are dropped.
func Apply(name string) (skipped []string, err error) {
	parent, err := Parent(name)
	if err != nil {
		return nil, err
	}
	upper, err := UpperDir(name)
	if err != nil {
		return nil, err
	}
	dataPath, err := DataDir(parent)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		return nil, err
	}
	dst, err := os.OpenRoot(dataPath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := os.ReadDir(filepath.Join(upper, rel))
		if err != nil {
			return err
		}
		for _, e := range entries {
			erel := filepath.Join(rel, e.Name())
			src := filepath.Join(upper, erel)
			fi, err := os.Lstat(src)
			if err != nil {
				return err
			}
			switch {
			case isWhiteout(fi):
				if err := dst.RemoveAll(erel); err != nil {
					return fmt.Errorf("delete %s: %w", erel, err)
				}
			case fi.IsDir():
				if isOpaque(src) {
					if err := dst.RemoveAll(erel); err != nil {
						return fmt.Errorf("replace %s: %w", erel, err)
					}
				} else if cur, err := dst.Lstat(erel); err == nil && !cur.IsDir() {
					if err := dst.Remove(erel); err != nil {
						return fmt.Errorf("replace %s: %w", erel, err)
					}
				}
				if err := dst.Mkdir(erel, fi.Mode().Perm()); err != nil && !errors.Is(err, fs.ErrExist) {
					return fmt.Errorf("mkdir %s: %w", erel, err)
				}
				if err := dst.Chmod(erel, fi.Mode().Perm()); err != nil {
					return fmt.Errorf("chmod %s: %w", erel, err)
				}
				if err := chownLike(dst, erel, fi); err != nil {
					return err
				}
				if err := walk(erel); err != nil {
					return err
				}
			case fi.Mode()&fs.ModeSymlink != 0:
				target, err := os.Readlink(src)
				if err != nil {
					return err
				}
				if err := dst.RemoveAll(erel); err != nil {
					return fmt.Errorf("replace %s: %w", erel, err)
				}
				if err := dst.Symlink(target, erel); err != nil {
					return fmt.Errorf("symlink %s: %w", erel, err)
				}
				if err := chownLike(dst, erel, fi); err != nil {
					return err
				}
			case fi.Mode().IsRegular():
				if cur, err := dst.Lstat(erel); err == nil && !cur.Mode().IsRegular() {
					if err := dst.RemoveAll(erel); err != nil {
						return fmt.Errorf("replace %s: %w", erel, err)
					}
				}
				if err := copyInto(dst, erel, src, fi.Mode().Perm()&^(fs.ModeSetuid|fs.ModeSetgid)); err != nil {
					return err
				}
				if err := chownLike(dst, erel, fi); err != nil {
					return err
				}
			default:
				skipped = append(skipped, erel)
			}
		}
		return nil
	}
	if err := walk("."); err != nil {
		return skipped, err
	}
	return skipped, Discard(name)
}

func copyInto(dst *os.Root, rel, src string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := dst.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	// The file may have existed with other bits; the upper's are the truth.
	return dst.Chmod(rel, perm)
}

// DescribeChanges renders a diff as `K path` lines, the way `git status
// --short` does.
func DescribeChanges(cs []Change) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "%s %s\n", c.Kind, c.Path)
	}
	return b.String()
}
