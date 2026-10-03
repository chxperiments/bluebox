//go:build !linux

package guest

import "errors"

// Init and Prepare only exist in a Linux guest.
func Init() {}

func Prepare(string, int64, bool) error { return errors.New("guest only") }
