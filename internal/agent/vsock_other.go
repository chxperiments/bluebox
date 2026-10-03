//go:build !linux

package agent

import "errors"

// ServeVsock only exists in a Linux guest.
func ServeVsock(string) error { return errors.New("vsock agent needs Linux") }
