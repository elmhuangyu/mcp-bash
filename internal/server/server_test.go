package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcp-bash/internal/config"
	"mcp-bash/internal/runner"
)

func setupTestHTTPServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	home := t.TempDir()
	r := runner.New("/bin/bash", home, 5*time.Second, 15*time.Second, 64*1024)
	if err := r.EnsureLocalBin(); err != nil {
		t.Fatalf("EnsureLocalBin() error: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mcpServer := NewMCPServer(r, logger)

	cfg := config.Config{
		Transport: config.TransportStreamableHTTP,
		MCPPath:   "/mcp",
		Stateless: false,
		HomeDir:   home,
	}
	mux := NewHTTPMux(cfg, mcpServer, logger)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, home
}

func TestHealthz(t *testing.T) {
	ts, _ := setupTestHTTPServer(t)

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if strings.TrimSpace(string(body)) != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", string(body), `{"status":"ok"}`)
	}
}

type customHostTransport struct {
	base http.RoundTripper
	host string
}

func (c customHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Host = c.host
	return c.base.RoundTrip(cloned)
}

func TestStreamableHTTPMCPServer(t *testing.T) {
	ts, _ := setupTestHTTPServer(t)
	ctx := context.Background()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp",
		HTTPClient: &http.Client{
			Transport: customHostTransport{
				base: http.DefaultTransport,
				host: "mcp-bash.lan:8080",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("client.Connect() failed (possible localhost protection failure): %v", err)
	}
	defer session.Close()

	initRes := session.InitializeResult()
	if initRes == nil {
		t.Fatalf("InitializeResult() is nil")
	}
	for _, want := range []string{
		"$HOME/.local/bin",
		"bash",
		"python3",
		"lightpanda",
		"git",
		"curl",
		"wget",
		"fd",
		"rg",
		"tar",
		"xz",
		"make",
		"just",
	} {
		if !strings.Contains(initRes.Instructions, want) {
			t.Errorf("Instructions missing %q:\n%s", want, initRes.Instructions)
		}
	}

	toolsRes, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(toolsRes.Tools) != 1 || toolsRes.Tools[0].Name != "bash" {
		t.Fatalf("unexpected tools: %+v", toolsRes.Tools)
	}

	callText := func(res *mcp.CallToolResult) string {
		t.Helper()
		if len(res.Content) == 0 {
			t.Fatalf("expected non-empty Content")
		}
		tc, ok := res.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("expected *mcp.TextContent, got %T", res.Content[0])
		}
		return tc.Text
	}

	resOK, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "bash",
		Arguments: map[string]any{"command": "echo hello-mcp"},
	})
	if err != nil {
		t.Fatalf("CallTool(echo) error: %v", err)
	}
	if resOK.IsError {
		t.Fatalf("CallTool(echo) IsError = true, want false; text=%s", callText(resOK))
	}
	textOK := callText(resOK)
	if !strings.Contains(textOK, "exit_code: 0") || !strings.Contains(textOK, "hello-mcp") {
		t.Errorf("unexpected echo output:\n%s", textOK)
	}

	customDir := t.TempDir()
	resCwd, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "bash",
		Arguments: map[string]any{"command": "pwd", "cwd": customDir},
	})
	if err != nil {
		t.Fatalf("CallTool(pwd) error: %v", err)
	}
	if resCwd.IsError {
		t.Fatalf("CallTool(pwd) IsError = true: %s", callText(resCwd))
	}
	if !strings.Contains(callText(resCwd), customDir) {
		t.Errorf("pwd output did not contain %q:\n%s", customDir, callText(resCwd))
	}

	resTimeout, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "bash",
		Arguments: map[string]any{"command": "sleep 10", "timeout_seconds": 1},
	})
	if err != nil {
		t.Fatalf("CallTool(timeout) error: %v", err)
	}
	if !resTimeout.IsError {
		t.Errorf("CallTool(timeout) IsError = false, want true")
	}
	if !strings.Contains(callText(resTimeout), "timed_out: true") {
		t.Errorf("expected timed_out: true in output:\n%s", callText(resTimeout))
	}

	resEmpty, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "bash",
		Arguments: map[string]any{"command": ""},
	})
	if err != nil {
		t.Fatalf("CallTool(empty) error: %v", err)
	}
	if !resEmpty.IsError {
		t.Errorf("CallTool(empty) IsError = false, want true")
	}
}
