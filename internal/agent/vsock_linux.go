package agent

import (
	"errors"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// ServeVsock runs the agent on AF_VSOCK, for a Firecracker guest: the host
// reaches it through the VM's vsock Unix socket, so no network is needed
// and nothing listens on an IP address at all.
func ServeVsock(token string) error {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: Port}); err != nil {
		return err
	}
	if err := unix.Listen(fd, 64); err != nil {
		return err
	}
	kernel, _ := KernelRelease()
	return serveOn(&vsockListener{fd: fd}, newTokenBox(token), kernel)
}

type vsockListener struct{ fd int }

func (l *vsockListener) Accept() (net.Conn, error) {
	for {
		nfd, _, err := unix.Accept4(l.fd, unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, err
		}
		// Non-blocking, so os.File registers it with the poller and
		// deadlines work.
		return &fileConn{os.NewFile(uintptr(nfd), "vsock")}, nil
	}
}

func (l *vsockListener) Close() error   { return unix.Close(l.fd) }
func (l *vsockListener) Addr() net.Addr { return vsockAddr{} }

// fileConn is a net.Conn over a vsock socket's *os.File; net.FileConn does
// not know AF_VSOCK.
type fileConn struct{ *os.File }

func (c *fileConn) LocalAddr() net.Addr  { return vsockAddr{} }
func (c *fileConn) RemoteAddr() net.Addr { return vsockAddr{} }
func (c *fileConn) SetDeadline(t time.Time) error {
	return c.File.SetDeadline(t)
}
func (c *fileConn) SetReadDeadline(t time.Time) error  { return c.File.SetReadDeadline(t) }
func (c *fileConn) SetWriteDeadline(t time.Time) error { return c.File.SetWriteDeadline(t) }

type vsockAddr struct{}

func (vsockAddr) Network() string { return "vsock" }
func (vsockAddr) String() string  { return "vsock" }
