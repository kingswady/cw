package mcpbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeMCP answers every request with the headers it came with, so tests can
// see what the bridge sent; answer overrides the reply per method.
func fakeMCP(t *testing.T, answer map[string]func(w http.ResponseWriter, id json.RawMessage)) (*httptest.Server, *[]http.Header) {
	t.Helper()
	var mu sync.Mutex
	seen := &[]http.Header{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*seen = append(*seen, r.Header.Clone())
		mu.Unlock()
		if r.URL.Path != "/mcp" || r.Header.Get("Authorization") != "Bearer "+goodToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &msg)
		if fn := answer[msg.Method]; fn != nil {
			fn(w, msg.ID)
			return
		}
		if len(msg.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": msg.ID,
			"result": map[string]any{"version": r.Header.Get("MCP-Protocol-Version"), "method": r.Header.Get("Mcp-Method"), "name": r.Header.Get("Mcp-Name")},
		})
	}))
	t.Cleanup(server.Close)
	return server, seen
}

const goodToken = "cwk_test-token-0123456789"

// runMCP feeds lines to a Bridge on server and returns its answers, sorted by id.
func runMCP(t *testing.T, server *httptest.Server, lines ...string) (*bytes.Buffer, []map[string]any) {
	t.Helper()
	var out, log bytes.Buffer
	b := &Bridge{Endpoint: server.URL + "/mcp", Token: goodToken, HTTP: server.Client(), UserAgent: "cw/test", Out: &out, Log: &log}
	if err := b.Serve(strings.NewReader(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatal(err)
	}
	var answers []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var answer map[string]any
		if err := json.Unmarshal([]byte(line), &answer); err != nil {
			t.Fatalf("not one JSON message per line: %q", line)
		}
		answers = append(answers, answer)
	}
	sort.Slice(answers, func(i, j int) bool { return fmt.Sprint(answers[i]["id"]) < fmt.Sprint(answers[j]["id"]) })
	return &log, answers
}

func TestMCPForwardsAModernCallWithItsHeaders(t *testing.T) {
	server, _ := fakeMCP(t, nil)
	_, answers := runMCP(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"apps_list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`)
	if len(answers) != 1 {
		t.Fatalf("answers: %v", answers)
	}
	result, _ := answers[0]["result"].(map[string]any)
	if result["version"] != "2026-07-28" || result["method"] != "tools/call" || result["name"] != "apps_list" {
		t.Errorf("headers sent: %v", result)
	}
}

func TestMCPRemembersTheVersionInitializeAgreed(t *testing.T) {
	server, seen := fakeMCP(t, map[string]func(http.ResponseWriter, json.RawMessage){
		"initialize": func(w http.ResponseWriter, id json.RawMessage) {
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"protocolVersion": "2025-06-18"}})
		},
	})
	// A 2025 client waits for initialize before anything else; so does this test.
	runMCP(t, server, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`)
	if got := (*seen)[0].Get("MCP-Protocol-Version"); got != "2025-11-25" {
		t.Errorf("initialize carries the version it offers, got %q", got)
	}
	b := &Bridge{version: "2025-06-18"}
	if got := b.versionFor(&mcpMessage{Method: "tools/list"}); got != "2025-06-18" {
		t.Errorf("later requests repeat the agreed version, got %q", got)
	}
}

func TestMCPNotificationsGetNoAnswer(t *testing.T) {
	server, _ := fakeMCP(t, nil)
	log, answers := runMCP(t, server, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(answers) != 0 || log.Len() != 0 {
		t.Errorf("answers %v, log %s", answers, log)
	}
}

func TestMCPTurnsAnythingButMCPIntoAnErrorForThatRequest(t *testing.T) {
	server, _ := fakeMCP(t, map[string]func(http.ResponseWriter, json.RawMessage){
		"tools/list": func(w http.ResponseWriter, _ json.RawMessage) {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("<html>502</html>"))
		},
	})
	_, answers := runMCP(t, server, `{"jsonrpc":"2.0","id":"a","method":"tools/list"}`, `not json`)
	if len(answers) != 2 {
		t.Fatalf("answers: %v", answers)
	}
	for _, answer := range answers {
		if answer["error"] == nil {
			t.Errorf("expected an error: %v", answer)
		}
	}
	if answers[1]["id"] != "a" || !strings.Contains(fmt.Sprint(answers[1]["error"].(map[string]any)["message"]), "HTTP 502") {
		t.Errorf("the failed request keeps its id and says why: %v", answers[1])
	}
}

func TestMCPPassesOnEachEventOfAStream(t *testing.T) {
	server, _ := fakeMCP(t, map[string]func(http.ResponseWriter, json.RawMessage){
		"tools/call": func(w http.ResponseWriter, id json.RawMessage) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, `data: {"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":1}}`+"\n\n")
			io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"id\":"+string(id)+",\"result\":{}}\n\n")
		},
	})
	_, answers := runMCP(t, server, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"x"}}`)
	if len(answers) != 2 {
		t.Fatalf("answers: %v", answers)
	}
}

func TestHeaderSafeValues(t *testing.T) {
	cases := map[string]string{
		"apps_list":          "apps_list",
		"Hello, 世界":          "=?base64?SGVsbG8sIOS4lueVjA==?=",
		" padded ":           "=?base64?IHBhZGRlZCA=?=",
		"=?base64?literal?=": "=?base64?PT9iYXNlNjQ/bGl0ZXJhbD89?=",
	}
	for in, want := range cases {
		if got := headerSafe(in); got != want {
			t.Errorf("headerSafe(%q) = %q, want %q", in, got, want)
		}
	}
}
