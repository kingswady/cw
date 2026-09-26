// Package mcpbridge serves the platform's MCP tools to a local AI client over
// stdio: each line on stdin is one JSON-RPC message, posted to <platform>/mcp
// with the token; each answer goes back as one line on stdout.
package mcpbridge

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// metaVersion is where a 2026-07-28 request names its protocol version.
const metaVersion = "io.modelcontextprotocol/protocolVersion"

// mcpNamed are the methods whose params.name (or uri) the Mcp-Name header repeats.
var mcpNamed = map[string]bool{"tools/call": true, "prompts/get": true, "resources/read": true}

// Bridge forwards one client's messages to one platform's /mcp.
type Bridge struct {
	Endpoint  string // <platform>/mcp
	Token     string
	HTTP      *http.Client
	UserAgent string
	Out, Log  io.Writer
	mu        sync.Mutex // guards Out and version
	// version is what a 2025 client agreed in initialize; later requests repeat it.
	version string
}

// Serve forwards every message until stdin closes, answering them concurrently
// (a client may run several tools at once).
func (b *Bridge) Serve(in io.Reader) error {
	reader := bufio.NewReaderSize(in, 1<<20)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		line, err := reader.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			wg.Add(1)
			go func(message []byte) {
				defer wg.Done()
				b.forward(message)
			}(line)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type mcpMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Name            string                     `json:"name"`
		URI             string                     `json:"uri"`
		ProtocolVersion string                     `json:"protocolVersion"`
		Meta            map[string]json.RawMessage `json:"_meta"`
	} `json:"params"`
}

func (b *Bridge) forward(raw []byte) {
	var msg mcpMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		b.reply(nil, -32700, "Parse error")
		return
	}
	req, err := http.NewRequest(http.MethodPost, b.Endpoint, bytes.NewReader(raw))
	if err != nil {
		b.reply(msg.ID, -32603, err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+b.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", b.UserAgent)
	req.Header.Set("Mcp-Method", msg.Method)
	if mcpNamed[msg.Method] {
		req.Header.Set("Mcp-Name", headerSafe(msg.Params.Name+msg.Params.URI))
	}
	if v := b.versionFor(&msg); v != "" {
		req.Header.Set("MCP-Protocol-Version", v)
	}
	resp, err := b.HTTP.Do(req)
	if err != nil {
		b.reply(msg.ID, -32603, fmt.Sprintf("cannot reach %s: %v", b.Endpoint, err))
		return
	}
	defer resp.Body.Close()
	b.relay(&msg, resp)
}

// versionFor is the MCP-Protocol-Version header: the request's own _meta version
// (2026-07-28), the one initialize offers, or the one initialize agreed.
func (b *Bridge) versionFor(msg *mcpMessage) string {
	var v string
	if raw, ok := msg.Params.Meta[metaVersion]; ok && json.Unmarshal(raw, &v) == nil && v != "" {
		return v
	}
	if msg.Method == "initialize" {
		return msg.Params.ProtocolVersion
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.version
}

func (b *Bridge) relay(msg *mcpMessage, resp *http.Response) {
	if len(msg.ID) == 0 || resp.StatusCode == http.StatusAccepted {
		return // a notification: nothing to answer
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		b.relayEvents(resp.Body)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	var answer struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if !json.Valid(body) || json.Unmarshal(body, &answer) != nil {
		b.reply(msg.ID, -32603, fmt.Sprintf("%s answered HTTP %d, not MCP — is the URL right?", b.Endpoint, resp.StatusCode))
		return
	}
	if msg.Method == "initialize" && answer.Result.ProtocolVersion != "" {
		b.mu.Lock()
		b.version = answer.Result.ProtocolVersion
		b.mu.Unlock()
	}
	b.write(body)
}

// relayEvents passes on each message of an SSE response, in order.
func (b *Bridge) relayEvents(body io.Reader) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 32<<20)
	var data bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case line == "" && data.Len() > 0:
			b.write(data.Bytes())
			data.Reset()
		}
	}
	if data.Len() > 0 {
		b.write(data.Bytes())
	}
}

func (b *Bridge) reply(id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	out, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message},
	})
	b.write(out)
}

// write puts one message on stdout as one line, as the stdio transport requires.
func (b *Bridge) write(message []byte) {
	var line bytes.Buffer
	if err := json.Compact(&line, message); err != nil {
		fmt.Fprintf(b.Log, "cw mcp: dropped a malformed answer: %v\n", err)
		return
	}
	line.WriteByte('\n')
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Out.Write(line.Bytes())
}

// headerSafe is a value as MCP carries it in a header: plain printable ASCII
// as is, anything else in the "=?base64?…?=" form.
func headerSafe(value string) string {
	plain := value == strings.TrimSpace(value) &&
		!(strings.HasPrefix(value, "=?base64?") && strings.HasSuffix(value, "?="))
	for _, r := range value {
		if r < 0x20 || r > 0x7e {
			plain = false
			break
		}
	}
	if plain {
		return value
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(value)) + "?="
}
