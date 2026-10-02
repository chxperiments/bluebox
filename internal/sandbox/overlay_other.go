//go:build !linux

package sandbox

import (
	"io/fs"
	"os"
)

// Forks are mounted by rootless podman's overlay support, which exists on
// Linux hosts only; these keep the package building elsewhere.

func isWhiteout(fs.FileInfo) bool { return false }

func isOpaque(string) bool { return false }

func chownLike(*os.Root, string, fs.FileInfo) error { return nil }
