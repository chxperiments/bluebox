package agent

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

const testToken = "secret"

func startAgent(t *testing.T, kernel string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go serveOn(ln, newTokenBox(testToken), kernel)
	return ln.Addr().String()
}

func call(t *testing.T, addr string, req Request, stdin string) (Result, string, string, error) {
	t.Helper()
	var out, errw bytes.Buffer
	var in io.Reader // a nil *strings.Reader would not be a nil io.Reader
	if stdin != "" {
		in = strings.NewReader(stdin)
	}
	if req.Token == "" {
		req.Token = testToken
	}
	res, err := Exec(addr, req, nil, in, &out, &errw)
	return res, out.String(), errw.String(), err
}

func TestPingReportsKernel(t *testing.T) {
	addr := startAgent(t, "6.12.91")
	res, _, _, err := call(t, addr, Request{}, "")
	if err != nil || res.Code != 0 || res.Kernel != "6.12.91" {
		t.Fatalf("ping = %+v, %v", res, err)
	}
}

func TestOutputAndExitCode(t *testing.T) {
	addr := startAgent(t, "k")
	res, out, errw, err := call(t, addr, Request{Argv: []string{"sh", "-c", "echo out; echo err >&2; exit 3"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 3 || out != "out\n" || errw != "err\n" {
		t.Fatalf("got code %d, stdout %q, stderr %q", res.Code, out, errw)
	}
}

func TestStdinIsForwarded(t *testing.T) {
	addr := startAgent(t, "k")
	_, out, _, err := call(t, addr, Request{Argv: []string{"cat"}}, "piped\n")
	if err != nil || out != "piped\n" {
		t.Fatalf("cat = %q, %v", out, err)
	}
}

func TestNoStdinMeansEOF(t *testing.T) {
	addr := startAgent(t, "k")
	// cat would block forever if stdin were left open.
	res, _, _, err := call(t, addr, Request{Argv: []string{"cat"}}, "")
	if err != nil || res.Code != 0 {
		t.Fatalf("cat = %+v, %v", res, err)
	}
}

func TestMissingCommand(t *testing.T) {
	addr := startAgent(t, "k")
	res, _, errw, err := call(t, addr, Request{Argv: []string{"no-such-command-xyz"}}, "")
	if err != nil || res.Code != 127 || !strings.Contains(errw, "not found") {
		t.Fatalf("got %+v, %q, %v", res, errw, err)
	}
}

func TestTimeoutKillsTheGroup(t *testing.T) {
	addr := startAgent(t, "k")
	res, _, _, err := call(t, addr, Request{Argv: []string{"sh", "-c", "sleep 30; sleep 30"}, TimeoutSeconds: 1}, "")
	if err != nil || !res.TimedOut || res.Code != ExitTimeout {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestSignalExitIsShellStyle(t *testing.T) {
	addr := startAgent(t, "k")
	res, _, _, err := call(t, addr, Request{Argv: []string{"sh", "-c", "kill -9 $$"}}, "")
	if err != nil || res.Code != 137 {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestWrongTokenGetsSilence(t *testing.T) {
	addr := startAgent(t, "k")
	_, _, _, err := call(t, addr, Request{Token: "wrong", Argv: []string{"true"}}, "")
	if err == nil {
		t.Fatal("a wrong token was accepted")
	}
}

func TestCheckRefusesBeforeRunning(t *testing.T) {
	addr := startAgent(t, "host-kernel")
	refused := errors.New("refused")
	var out, errw bytes.Buffer
	_, err := Exec(addr, Request{Token: testToken, Argv: []string{"echo", "ran"}},
		func(string) error { return refused }, nil, &out, &errw)
	if !errors.Is(err, refused) || out.Len() != 0 {
		t.Fatalf("err %v, stdout %q", err, out.String())
	}
}

func TestSilentPeersCannotStarveTheOwner(t *testing.T) {
	addr := startAgent(t, "k")
	// Fill every unauthenticated slot with a peer that never says hello.
	var idle []net.Conn
	for range maxUnauthenticated * 2 {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		idle = append(idle, c)
	}
	defer func() {
		for _, c := range idle {
			c.Close()
		}
	}()
	// Once the silent peers time out, the owner gets through.
	deadline := time.Now().Add(2*helloTimeout + time.Second)
	for {
		res, _, _, err := call(t, addr, Request{Argv: []string{"true"}}, "")
		if err == nil && res.Code == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("owner still locked out: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestTokenRotation(t *testing.T) {
	addr := startAgent(t, "k")
	if _, _, _, err := call(t, addr, Request{NewToken: "rotated"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := call(t, addr, Request{Argv: []string{"true"}}, ""); err == nil {
		t.Fatal("the old token still works after rotation")
	}
	if res, _, _, err := call(t, addr, Request{Token: "rotated", Argv: []string{"true"}}, ""); err != nil || res.Code != 0 {
		t.Fatalf("the new token does not work: %+v %v", res, err)
	}
}
