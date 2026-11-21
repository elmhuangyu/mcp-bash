package config

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	TransportStreamableHTTP = "streamable-http"
	TransportSSE            = "sse"
	TransportStdio          = "stdio"
)

// Config holds the runtime configuration for the mcp-bash server.
type Config struct {
	Transport      string
	Addr           string
	MCPPath        string
	Stateless      bool
	HomeDir        string
	BashPath       string
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
	MaxOutputBytes int
}

// LoadFromEnv loads configuration from environment variables and validates all settings.
func LoadFromEnv() (Config, error) {
	transport := strings.ToLower(strings.TrimSpace(os.Getenv("MCP_TRANSPORT")))
	if transport == "" {
		transport = TransportStreamableHTTP
	}
	if transport == "http" {
		transport = TransportStreamableHTTP
	}
	switch transport {
	case TransportStreamableHTTP, TransportSSE, TransportStdio:
	default:
		return Config{}, fmt.Errorf("invalid MCP_TRANSPORT %q: must be streamable-http, sse, or stdio", transport)
	}

	addr := strings.TrimSpace(os.Getenv("MCP_ADDR"))
	if addr == "" {
		addr = ":8080"
	}

	mcpPath := strings.TrimSpace(os.Getenv("MCP_PATH"))
	if mcpPath == "" {
		switch transport {
		case TransportStreamableHTTP:
			mcpPath = "/mcp"
		case TransportSSE:
			mcpPath = "/sse"
		}
	}
	if mcpPath != "" && !strings.HasPrefix(mcpPath, "/") {
		mcpPath = "/" + mcpPath
	}

	stateless := false
	if raw := strings.TrimSpace(os.Getenv("MCP_STATELESS")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid MCP_STATELESS %q: %w", raw, err)
		}
		stateless = parsed
	}

	homeDir := strings.TrimSpace(os.Getenv("HOME"))
	if homeDir == "" {
		if h, err := os.UserHomeDir(); err == nil && strings.TrimSpace(h) != "" {
			homeDir = h
		} else {
			homeDir = "/home/agent"
		}
	}

	bashPath := strings.TrimSpace(os.Getenv("MCP_BASH_PATH"))
	if bashPath == "" {
		if _, err := os.Stat("/bin/bash"); err == nil {
			bashPath = "/bin/bash"
		} else {
			found, lookErr := exec.LookPath("bash")
			if lookErr != nil {
				return Config{}, fmt.Errorf("locate bash binary: %w", lookErr)
			}
			bashPath = found
		}
	}

	defaultTimeoutSec, err := parsePositiveIntEnv("MCP_DEFAULT_TIMEOUT_SECONDS", 60)
	if err != nil {
		return Config{}, err
	}
	maxTimeoutSec, err := parsePositiveIntEnv("MCP_MAX_TIMEOUT_SECONDS", 600)
	if err != nil {
		return Config{}, err
	}
	if maxTimeoutSec < defaultTimeoutSec {
		return Config{}, fmt.Errorf("MCP_MAX_TIMEOUT_SECONDS (%d) must be >= MCP_DEFAULT_TIMEOUT_SECONDS (%d)", maxTimeoutSec, defaultTimeoutSec)
	}

	maxOutputBytes, err := parsePositiveIntEnv("MCP_MAX_OUTPUT_BYTES", 131072)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Transport:      transport,
		Addr:           addr,
		MCPPath:        mcpPath,
		Stateless:      stateless,
		HomeDir:        homeDir,
		BashPath:       bashPath,
		DefaultTimeout: time.Duration(defaultTimeoutSec) * time.Second,
		MaxTimeout:     time.Duration(maxTimeoutSec) * time.Second,
		MaxOutputBytes: maxOutputBytes,
	}, nil
}

func parsePositiveIntEnv(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, raw, err)
	}
	if val <= 0 {
		return 0, fmt.Errorf("invalid %s (%d): must be > 0", key, val)
	}
	return val, nil
}
