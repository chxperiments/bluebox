package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// fakeAPI answers like the local API for a sandbox named "ok".
func fakeAPI() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sandboxes", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"name":"ok","up":false}]`))
	})
	mux.HandleFunc("POST /v1/sandboxes/{name}/run", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("name") != "ok" {
			w.WriteHeader(404)
			w.Write([]byte(`{"error":"no sandbox \"` + r.PathValue("name") + `\"","code":"no_sandbox"}`))
			return
		}
		w.Write([]byte(`{"exit_code":3,"stdout":"aGkK","stderr":"","duration_ms":41}`))
	})
	return mux
}

func session(t *testing.T, allowApply bool, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := New(fakeAPI(), "test", allowApply).Serve(strings.NewReader(strings.Join(lines, "\n")), &out); err != nil {
		t.Fatal(err)
	}
	var res []map[string]any
	dec := json.NewDecoder(&out)
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		res = append(res, m)
	}
	return res
}

func TestHandshakeAndTools(t *testing.T) {
	res := session(t, false,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(res) != 2 {
		t.Fatalf("want 2 responses (the notification gets none), got %d", len(res))
	}
	info := res[0]["result"].(map[string]any)
	if info["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v", info["protocolVersion"])
	}
	names := map[string]bool{}
	for _, tl := range res[1]["result"].(map[string]any)["tools"].([]any) {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"list_sandboxes", "run", "exec", "up", "down", "fork", "diff", "discard", "read_file", "write_file"} {
		if !names[want] {
			t.Errorf("tool %s missing", want)
		}
	}
	if names["apply"] {
		t.Error("apply must not be offered unless allowed: it would let an agent skip the human review of its fork")
	}
	if !toolNames(session(t, true, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))["apply"] {
		t.Error("apply should be offered with --allow-apply")
	}
}

func toolNames(res []map[string]any) map[string]bool {
	names := map[string]bool{}
	for _, tl := range res[0]["result"].(map[string]any)["tools"].([]any) {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	return names
}

func TestRunResultAndErrors(t *testing.T) {
	res := session(t, false,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run","arguments":{"sandbox":"ok","command":"echo hi; exit 3"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run","arguments":{"sandbox":"ghost","command":"true"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"apply","arguments":{"sandbox":"x"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"nope"}`,
	)
	ok := res[0]["result"].(map[string]any)
	text := ok["content"].([]any)[0].(map[string]any)["text"].(string)
	if ok["isError"] != false || !strings.Contains(text, "exit code 3") || !strings.Contains(text, "hi") {
		t.Errorf("a non-zero exit is a result, not a tool error: %v %q", ok["isError"], text)
	}
	bad := res[1]["result"].(map[string]any)
	if bad["isError"] != true || !strings.Contains(bad["content"].([]any)[0].(map[string]any)["text"].(string), "no sandbox") {
		t.Errorf("unknown sandbox should be a tool error: %v", bad)
	}
	if res[2]["error"] == nil {
		t.Error("calling apply without --allow-apply must fail")
	}
	if res[3]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Error("unknown method should be -32601")
	}
}
