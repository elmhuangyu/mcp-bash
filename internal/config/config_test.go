package config

import (
	"testing"
	"time"
)

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MCP_TRANSPORT",
		"MCP_ADDR",
		"MCP_BASE_PATH",
		"MCP_PATH",
		"MCP_STATELESS",
		"MCP_BASH_PATH",
		"MCP_DEFAULT_TIMEOUT_SECONDS",
		"MCP_MAX_TIMEOUT_SECONDS",
		"MCP_MAX_OUTPUT_BYTES",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadFromEnvDefaults(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("HOME", "/home/agent")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() unexpected error: %v", err)
	}

	if cfg.Transport != TransportStreamableHTTP {
		t.Errorf("Transport = %q, want %q", cfg.Transport, TransportStreamableHTTP)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":8080")
	}
	if cfg.BasePath != "" {
		t.Errorf("BasePath = %q, want empty", cfg.BasePath)
	}
	if cfg.MCPPath != "/mcp" {
		t.Errorf("MCPPath = %q, want %q", cfg.MCPPath, "/mcp")
	}
	if cfg.MCPRoute() != "/mcp" {
		t.Errorf("MCPRoute() = %q, want %q", cfg.MCPRoute(), "/mcp")
	}
	if cfg.HealthRoute() != "/healthz" {
		t.Errorf("HealthRoute() = %q, want %q", cfg.HealthRoute(), "/healthz")
	}
	if cfg.Stateless {
		t.Errorf("Stateless = true, want false")
	}
	if cfg.HomeDir != "/home/agent" {
		t.Errorf("HomeDir = %q, want %q", cfg.HomeDir, "/home/agent")
	}
	if cfg.DefaultTimeout != 60*time.Second {
		t.Errorf("DefaultTimeout = %v, want 60s", cfg.DefaultTimeout)
	}
	if cfg.MaxTimeout != 600*time.Second {
		t.Errorf("MaxTimeout = %v, want 600s", cfg.MaxTimeout)
	}
	if cfg.MaxOutputBytes != 131072 {
		t.Errorf("MaxOutputBytes = %d, want 131072", cfg.MaxOutputBytes)
	}
	if cfg.BashPath == "" {
		t.Errorf("BashPath is empty")
	}
}

func TestLoadFromEnvOverridesAndNormalization(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("MCP_TRANSPORT", "HTTP")
	t.Setenv("MCP_ADDR", "127.0.0.1:9090")
	t.Setenv("MCP_BASE_PATH", "team/instance-1/")
	t.Setenv("MCP_PATH", "custom-mcp")
	t.Setenv("MCP_STATELESS", "true")
	t.Setenv("MCP_BASH_PATH", "/usr/bin/bash")
	t.Setenv("MCP_DEFAULT_TIMEOUT_SECONDS", "30")
	t.Setenv("MCP_MAX_TIMEOUT_SECONDS", "120")
	t.Setenv("MCP_MAX_OUTPUT_BYTES", "4096")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() unexpected error: %v", err)
	}

	if cfg.Transport != TransportStreamableHTTP {
		t.Errorf("Transport = %q, want %q", cfg.Transport, TransportStreamableHTTP)
	}
	if cfg.Addr != "127.0.0.1:9090" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "127.0.0.1:9090")
	}
	if cfg.BasePath != "/team/instance-1" {
		t.Errorf("BasePath = %q, want %q", cfg.BasePath, "/team/instance-1")
	}
	if cfg.MCPPath != "/custom-mcp" {
		t.Errorf("MCPPath = %q, want %q", cfg.MCPPath, "/custom-mcp")
	}
	if cfg.MCPRoute() != "/team/instance-1/custom-mcp" {
		t.Errorf("MCPRoute() = %q, want %q", cfg.MCPRoute(), "/team/instance-1/custom-mcp")
	}
	if cfg.HealthRoute() != "/team/instance-1/healthz" {
		t.Errorf("HealthRoute() = %q, want %q", cfg.HealthRoute(), "/team/instance-1/healthz")
	}
	if !cfg.Stateless {
		t.Errorf("Stateless = false, want true")
	}
	if cfg.BashPath != "/usr/bin/bash" {
		t.Errorf("BashPath = %q, want %q", cfg.BashPath, "/usr/bin/bash")
	}
	if cfg.DefaultTimeout != 30*time.Second {
		t.Errorf("DefaultTimeout = %v, want 30s", cfg.DefaultTimeout)
	}
	if cfg.MaxTimeout != 120*time.Second {
		t.Errorf("MaxTimeout = %v, want 120s", cfg.MaxTimeout)
	}
	if cfg.MaxOutputBytes != 4096 {
		t.Errorf("MaxOutputBytes = %d, want 4096", cfg.MaxOutputBytes)
	}
}

func TestLoadFromEnvSSEDefaultPath(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("MCP_TRANSPORT", "sse")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() unexpected error: %v", err)
	}
	if cfg.Transport != TransportSSE {
		t.Errorf("Transport = %q, want %q", cfg.Transport, TransportSSE)
	}
	if cfg.MCPPath != "/sse" {
		t.Errorf("MCPPath = %q, want %q", cfg.MCPPath, "/sse")
	}
}

func TestLoadFromEnvBasePathAndRoutes(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantBase    string
		wantMCPPath string
		wantMCP     string
		wantHealth  string
	}{
		{
			name:        "default empty base path",
			env:         map[string]string{},
			wantBase:    "",
			wantMCPPath: "/mcp",
			wantMCP:     "/mcp",
			wantHealth:  "/healthz",
		},
		{
			name:        "root slash base path normalizes to empty",
			env:         map[string]string{"MCP_BASE_PATH": "/"},
			wantBase:    "",
			wantMCPPath: "/mcp",
			wantMCP:     "/mcp",
			wantHealth:  "/healthz",
		},
		{
			name:        "multiple slashes base path normalizes to empty",
			env:         map[string]string{"MCP_BASE_PATH": "///"},
			wantBase:    "",
			wantMCPPath: "/mcp",
			wantMCP:     "/mcp",
			wantHealth:  "/healthz",
		},
		{
			name:        "base path without leading slash",
			env:         map[string]string{"MCP_BASE_PATH": "app1"},
			wantBase:    "/app1",
			wantMCPPath: "/mcp",
			wantMCP:     "/app1/mcp",
			wantHealth:  "/app1/healthz",
		},
		{
			name:        "base path with trailing slash",
			env:         map[string]string{"MCP_BASE_PATH": "/app1/"},
			wantBase:    "/app1",
			wantMCPPath: "/mcp",
			wantMCP:     "/app1/mcp",
			wantHealth:  "/app1/healthz",
		},
		{
			name:        "nested base path with repeated slashes",
			env:         map[string]string{"MCP_BASE_PATH": "/team//app1/"},
			wantBase:    "/team/app1",
			wantMCPPath: "/mcp",
			wantMCP:     "/team/app1/mcp",
			wantHealth:  "/team/app1/healthz",
		},
		{
			name:        "base path with sse transport",
			env:         map[string]string{"MCP_BASE_PATH": "/app1", "MCP_TRANSPORT": "sse"},
			wantBase:    "/app1",
			wantMCPPath: "/sse",
			wantMCP:     "/app1/sse",
			wantHealth:  "/app1/healthz",
		},
		{
			name:        "base path with root mcp path",
			env:         map[string]string{"MCP_BASE_PATH": "/app1", "MCP_PATH": "/"},
			wantBase:    "/app1",
			wantMCPPath: "/",
			wantMCP:     "/app1",
			wantHealth:  "/app1/healthz",
		},
		{
			name:        "empty base path with root mcp path",
			env:         map[string]string{"MCP_BASE_PATH": "", "MCP_PATH": "/"},
			wantBase:    "",
			wantMCPPath: "/",
			wantMCP:     "/",
			wantHealth:  "/healthz",
		},
		{
			name:        "base path with custom mcp path and trailing slash",
			env:         map[string]string{"MCP_BASE_PATH": "/app1", "MCP_PATH": "custom/"},
			wantBase:    "/app1",
			wantMCPPath: "/custom",
			wantMCP:     "/app1/custom",
			wantHealth:  "/app1/healthz",
		},
		{
			name:        "base path with stdio transport",
			env:         map[string]string{"MCP_BASE_PATH": "/app1", "MCP_TRANSPORT": "stdio"},
			wantBase:    "/app1",
			wantMCPPath: "",
			wantMCP:     "",
			wantHealth:  "/app1/healthz",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := LoadFromEnv()
			if err != nil {
				t.Fatalf("LoadFromEnv() unexpected error: %v", err)
			}
			if cfg.BasePath != tc.wantBase {
				t.Errorf("BasePath = %q, want %q", cfg.BasePath, tc.wantBase)
			}
			if cfg.MCPPath != tc.wantMCPPath {
				t.Errorf("MCPPath = %q, want %q", cfg.MCPPath, tc.wantMCPPath)
			}
			if cfg.MCPRoute() != tc.wantMCP {
				t.Errorf("MCPRoute() = %q, want %q", cfg.MCPRoute(), tc.wantMCP)
			}
			if cfg.HealthRoute() != tc.wantHealth {
				t.Errorf("HealthRoute() = %q, want %q", cfg.HealthRoute(), tc.wantHealth)
			}
		})
	}
}

func TestLoadFromEnvErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "invalid transport",
			env:  map[string]string{"MCP_TRANSPORT": "grpc"},
		},
		{
			name: "invalid stateless",
			env:  map[string]string{"MCP_STATELESS": "notabool"},
		},
		{
			name: "invalid default timeout",
			env:  map[string]string{"MCP_DEFAULT_TIMEOUT_SECONDS": "abc"},
		},
		{
			name: "zero default timeout",
			env:  map[string]string{"MCP_DEFAULT_TIMEOUT_SECONDS": "0"},
		},
		{
			name: "max timeout less than default",
			env: map[string]string{
				"MCP_DEFAULT_TIMEOUT_SECONDS": "100",
				"MCP_MAX_TIMEOUT_SECONDS":     "50",
			},
		},
		{
			name: "negative max output bytes",
			env:  map[string]string{"MCP_MAX_OUTPUT_BYTES": "-1"},
		},
		{
			name: "base path full url",
			env:  map[string]string{"MCP_BASE_PATH": "http://example.com:8080/path"},
		},
		{
			name: "base path query string",
			env:  map[string]string{"MCP_BASE_PATH": "/app?x=1"},
		},
		{
			name: "base path fragment",
			env:  map[string]string{"MCP_BASE_PATH": "/app#x"},
		},
		{
			name: "base path whitespace",
			env:  map[string]string{"MCP_BASE_PATH": "/app 1"},
		},
		{
			name: "base path braces",
			env:  map[string]string{"MCP_BASE_PATH": "/app/{id}"},
		},
		{
			name: "base path dotdot segment",
			env:  map[string]string{"MCP_BASE_PATH": "/app/../x"},
		},
		{
			name: "base path dot segment",
			env:  map[string]string{"MCP_BASE_PATH": "/app/./x"},
		},
		{
			name: "base path invalid percent encoding",
			env:  map[string]string{"MCP_BASE_PATH": "/app%ZZ"},
		},
		{
			name: "base path backslash",
			env:  map[string]string{"MCP_BASE_PATH": `/app\x`},
		},
		{
			name: "mcp path full url",
			env:  map[string]string{"MCP_PATH": "https://example.com/mcp"},
		},
		{
			name: "mcp path whitespace",
			env:  map[string]string{"MCP_PATH": "/my mcp"},
		},
		{
			name: "mcp path braces",
			env:  map[string]string{"MCP_PATH": "/mcp/{id}"},
		},
		{
			name: "mcp path dotdot segment",
			env:  map[string]string{"MCP_PATH": "/mcp/../other"},
		},
		{
			name: "mcp path query string",
			env:  map[string]string{"MCP_PATH": "/mcp?foo=bar"},
		},
		{
			name: "mcp path conflicts with healthz at root",
			env:  map[string]string{"MCP_PATH": "/healthz"},
		},
		{
			name: "mcp path conflicts with healthz under base path",
			env: map[string]string{
				"MCP_BASE_PATH": "/app1",
				"MCP_PATH":      "/healthz/",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := LoadFromEnv(); err == nil {
				t.Fatalf("LoadFromEnv() expected error, got nil")
			}
		})
	}
}
