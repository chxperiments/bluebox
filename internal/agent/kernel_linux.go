package agent

import "syscall"

// KernelRelease is uname -r, read from uname(2) directly: the host checks it
// before every exec, and a syscall is free where forking uname(1) is not.
func KernelRelease() (string, error) {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return "", err
	}
	b := make([]byte, 0, len(u.Release))
	for _, c := range u.Release {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b), nil
}
