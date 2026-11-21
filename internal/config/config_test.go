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
	if cfg.MCPPath != "/mcp" {
		t.Errorf("MCPPath = %q, want %q", cfg.MCPPath, "/mcp")
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
	if cfg.MCPPath != "/custom-mcp" {
		t.Errorf("MCPPath = %q, want %q", cfg.MCPPath, "/custom-mcp")
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
