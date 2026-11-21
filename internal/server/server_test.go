package server

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elmhuangyu/mcp-bash/internal/config"
	"github.com/elmhuangyu/mcp-bash/internal/runner"
)

func setupTestHTTPServerWithConfig(t *testing.T, cfg config.Config) (*httptest.Server, string) {
	t.Helper()
	home := cfg.HomeDir
	if home == "" {
		home = t.TempDir()
		cfg.HomeDir = home
	}

	r := runner.New("/bin/bash", home, 5*time.Second, 15*time.Second, 64*1024)
	if err := r.EnsureLocalBin(); err != nil {
		t.Fatalf("EnsureLocalBin() error: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mcpServer := NewMCPServer(r, logger)

	mux := NewHTTPMux(cfg, mcpServer, logger)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, home
}

func setupTestHTTPServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	return setupTestHTTPServerWithConfig(t, config.Config{
		Transport: config.TransportStreamableHTTP,
		MCPPath:   "/mcp",
		Stateless: false,
	})
}

func callText(t *testing.T, res *mcp.CallToolResult) string {
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

func assertHealthOK(t *testing.T, client *http.Client, url string) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s error: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll(%s) error: %v", url, err)
	}
	if strings.TrimSpace(string(body)) != `{"status":"ok"}` {
		t.Errorf("GET %s body = %q, want %q", url, string(body), `{"status":"ok"}`)
	}
}

func assertStatus(t *testing.T, client *http.Client, method, url string, wantStatus int) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s, %s) error: %v", method, url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s error: %v", method, url, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Errorf("%s %s status = %d, want %d", method, url, resp.StatusCode, wantStatus)
	}
}

func TestHealthz(t *testing.T) {
	ts, _ := setupTestHTTPServer(t)

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = session.Close() }()

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
		"gh",
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

func TestBasePathStreamableHTTP(t *testing.T) {
	ts, _ := setupTestHTTPServerWithConfig(t, config.Config{
		Transport: config.TransportStreamableHTTP,
		BasePath:  "/instance-1",
		MCPPath:   "/mcp",
	})

	assertHealthOK(t, ts.Client(), ts.URL+"/instance-1/healthz")
	assertStatus(t, ts.Client(), http.MethodGet, ts.URL+"/healthz", http.StatusNotFound)
	assertStatus(t, ts.Client(), http.MethodPost, ts.URL+"/mcp", http.StatusNotFound)

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/instance-1/mcp",
		HTTPClient: &http.Client{
			Transport: customHostTransport{
				base: http.DefaultTransport,
				host: "mcp.example.com",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("client.Connect() error: %v", err)
	}
	defer func() { _ = session.Close() }()

	toolsRes, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(toolsRes.Tools) != 1 || toolsRes.Tools[0].Name != "bash" {
		t.Fatalf("unexpected tools: %+v", toolsRes.Tools)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "bash",
		Arguments: map[string]any{"command": "echo hello-basepath"},
	})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool() IsError = true: %s", callText(t, res))
	}
	if text := callText(t, res); !strings.Contains(text, "hello-basepath") {
		t.Errorf("unexpected output:\n%s", text)
	}
}

func TestBasePathDirectRootMCPPath(t *testing.T) {
	ts, _ := setupTestHTTPServerWithConfig(t, config.Config{
		Transport: config.TransportStreamableHTTP,
		BasePath:  "/instance-2",
		MCPPath:   "/",
	})

	assertHealthOK(t, ts.Client(), ts.URL+"/instance-2/healthz")
	assertStatus(t, ts.Client(), http.MethodGet, ts.URL+"/instance-2/unknown", http.StatusNotFound)

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/instance-2",
	}, nil)
	if err != nil {
		t.Fatalf("client.Connect() error: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "bash",
		Arguments: map[string]any{"command": "echo direct-path"},
	})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool() IsError = true: %s", callText(t, res))
	}
	if text := callText(t, res); !strings.Contains(text, "direct-path") {
		t.Errorf("unexpected output:\n%s", text)
	}
}

func TestRootMCPPathExactMatch(t *testing.T) {
	ts, _ := setupTestHTTPServerWithConfig(t, config.Config{
		Transport: config.TransportStreamableHTTP,
		BasePath:  "",
		MCPPath:   "/",
	})

	assertHealthOK(t, ts.Client(), ts.URL+"/healthz")
	assertStatus(t, ts.Client(), http.MethodGet, ts.URL+"/unknown", http.StatusNotFound)

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/",
	}, nil)
	if err != nil {
		t.Fatalf("client.Connect() error: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "bash",
		Arguments: map[string]any{"command": "echo root-path"},
	})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool() IsError = true: %s", callText(t, res))
	}
	if text := callText(t, res); !strings.Contains(text, "root-path") {
		t.Errorf("unexpected output:\n%s", text)
	}
}

func TestBasePathSSE(t *testing.T) {
	ts, _ := setupTestHTTPServerWithConfig(t, config.Config{
		Transport: config.TransportSSE,
		BasePath:  "/sse-app",
		MCPPath:   "/sse",
	})

	assertHealthOK(t, ts.Client(), ts.URL+"/sse-app/healthz")
	assertStatus(t, ts.Client(), http.MethodGet, ts.URL+"/sse", http.StatusNotFound)

	reqCtx, cancelReq := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, ts.URL+"/sse-app/sse", nil)
	if err != nil {
		cancelReq()
		t.Fatalf("NewRequest(SSE) error: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		cancelReq()
		t.Fatalf("GET /sse-app/sse error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		cancelReq()
		_ = resp.Body.Close()
		t.Fatalf("GET /sse-app/sse status = %d, want 200", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	var eventLine, dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventLine = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			break
		}
	}
	scanErr := scanner.Err()
	cancelReq()
	_ = resp.Body.Close()
	if scanErr != nil {
		t.Fatalf("scanner error reading SSE stream: %v", scanErr)
	}
	if eventLine != "endpoint" {
		t.Fatalf("SSE initial event = %q, want %q", eventLine, "endpoint")
	}
	if !strings.HasPrefix(dataLine, "/sse-app/sse?sessionid=") {
		t.Fatalf("SSE endpoint event data = %q, want prefix %q", dataLine, "/sse-app/sse?sessionid=")
	}

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-sse-client",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &mcp.SSEClientTransport{
		Endpoint: ts.URL + "/sse-app/sse",
		HTTPClient: &http.Client{
			Transport: customHostTransport{
				base: http.DefaultTransport,
				host: "mcp.example.com",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("SSE client.Connect() error: %v", err)
	}
	defer func() { _ = session.Close() }()

	toolsRes, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(toolsRes.Tools) != 1 || toolsRes.Tools[0].Name != "bash" {
		t.Fatalf("unexpected tools: %+v", toolsRes.Tools)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "bash",
		Arguments: map[string]any{"command": "echo hello-sse"},
	})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool() IsError = true: %s", callText(t, res))
	}
	if text := callText(t, res); !strings.Contains(text, "hello-sse") {
		t.Errorf("unexpected output:\n%s", text)
	}
}

func TestMultipleInstancesOnOneDomain(t *testing.T) {
	homeA := t.TempDir()
	homeB := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	newMux := func(basePath, home string) http.Handler {
		r := runner.New("/bin/bash", home, 5*time.Second, 15*time.Second, 64*1024)
		if err := r.EnsureLocalBin(); err != nil {
			t.Fatalf("EnsureLocalBin(%s) error: %v", home, err)
		}
		srv := NewMCPServer(r, logger)
		return NewHTTPMux(config.Config{
			Transport: config.TransportStreamableHTTP,
			BasePath:  basePath,
			MCPPath:   "/mcp",
			HomeDir:   home,
		}, srv, logger)
	}

	outer := http.NewServeMux()
	outer.Handle("/inst-a/", newMux("/inst-a", homeA))
	outer.Handle("/inst-b/", newMux("/inst-b", homeB))

	ts := httptest.NewServer(outer)
	t.Cleanup(ts.Close)

	assertHealthOK(t, ts.Client(), ts.URL+"/inst-a/healthz")
	assertHealthOK(t, ts.Client(), ts.URL+"/inst-b/healthz")
	assertStatus(t, ts.Client(), http.MethodGet, ts.URL+"/healthz", http.StatusNotFound)

	ctx := context.Background()
	checkInstance := func(endpoint, wantHome string) {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{
			Name:    "test-client",
			Version: "0.1.0",
		}, nil)
		session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint: endpoint,
			HTTPClient: &http.Client{
				Transport: customHostTransport{
					base: http.DefaultTransport,
					host: "mcp.example.com",
				},
			},
		}, nil)
		if err != nil {
			t.Fatalf("client.Connect(%s) error: %v", endpoint, err)
		}
		defer func() { _ = session.Close() }()

		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "bash",
			Arguments: map[string]any{"command": "pwd"},
		})
		if err != nil {
			t.Fatalf("CallTool(%s) error: %v", endpoint, err)
		}
		if res.IsError {
			t.Fatalf("CallTool(%s) IsError = true: %s", endpoint, callText(t, res))
		}
		if text := callText(t, res); !strings.Contains(text, wantHome) {
			t.Errorf("pwd at %s = %q, want %q", endpoint, text, wantHome)
		}
	}

	checkInstance(ts.URL+"/inst-a/mcp", homeA)
	checkInstance(ts.URL+"/inst-b/mcp", homeB)
}
