package agent

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"
)

// dial connects to an agent. A plain host:port is TCP (the podman and krun
// backends publish the agent on loopback). "unix:<path>" is a Firecracker
// vsock device's host side: a Unix socket where the host names the guest
// port with "CONNECT <port>\n" and gets "OK <n>\n" back before the stream
// starts. The socket's permissions are the access control on that side.
func dial(addr string) (net.Conn, error) {
	path, ok := strings.CutPrefix(addr, "unix:")
	if !ok {
		return net.DialTimeout("tcp", addr, 2*time.Second)
	}
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(c, "CONNECT %d\n", Port); err != nil {
		c.Close()
		return nil, err
	}
	// Read the reply a byte at a time, so nothing of the agent's own stream
	// is consumed into a buffer that is then thrown away.
	r := bufio.NewReaderSize(oneByte{c}, 16)
	line, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "OK ") {
		c.Close()
		return nil, fmt.Errorf("vsock handshake: %q %v", strings.TrimSpace(line), err)
	}
	c.SetDeadline(time.Time{})
	return c, nil
}

type oneByte struct{ c net.Conn }

func (o oneByte) Read(p []byte) (int, error) { return o.c.Read(p[:1]) }
