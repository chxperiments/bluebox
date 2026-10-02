//go:build !linux

package runtime

import "errors"

func MountOverlay(string) error { return errors.New("forks need a Linux host") }
