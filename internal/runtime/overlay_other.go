//go:build !linux

package runtime

import "errors"

func MountOverlay(string) error { return errors.New("forks need a Linux host") }

func mountOverlay(_, _, _, _ string) error { return errors.New("overlays need a Linux host") }
