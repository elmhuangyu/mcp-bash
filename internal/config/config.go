package config

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
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
	BasePath       string
	MCPPath        string
	AccessToken    string
	Stateless      bool
	HomeDir        string
	BashPath       string
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
	MaxOutputBytes int
}

// HealthRoute returns the normalized HTTP path for the health check endpoint.
func (c Config) HealthRoute() string {
	return joinHTTPPath(c.BasePath, "/healthz")
}

// MCPRoute returns the normalized HTTP path for the MCP transport endpoint,
// or an empty string if MCPPath is unset (for example, in stdio mode).
func (c Config) MCPRoute() string {
	if strings.TrimSpace(c.MCPPath) == "" {
		return ""
	}
	return joinHTTPPath(c.BasePath, c.MCPPath)
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

	rawBasePath := strings.TrimSpace(os.Getenv("MCP_BASE_PATH"))
	if rawBasePath != "" {
		if err := validateHTTPPath("MCP_BASE_PATH", rawBasePath); err != nil {
			return Config{}, err
		}
	}
	basePath := cleanBasePath(rawBasePath)

	rawMCPPath := strings.TrimSpace(os.Getenv("MCP_PATH"))
	var mcpPath string
	if rawMCPPath == "" {
		switch transport {
		case TransportStreamableHTTP:
			mcpPath = "/mcp"
		case TransportSSE:
			mcpPath = "/sse"
		}
	} else {
		if err := validateHTTPPath("MCP_PATH", rawMCPPath); err != nil {
			return Config{}, err
		}
		mcpPath = cleanRoutePath(rawMCPPath)
	}

	accessToken := strings.TrimSpace(os.Getenv("MCP_ACCESS_TOKEN"))

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

	cfg := Config{
		Transport:      transport,
		Addr:           addr,
		BasePath:       basePath,
		MCPPath:        mcpPath,
		AccessToken:    accessToken,
		Stateless:      stateless,
		HomeDir:        homeDir,
		BashPath:       bashPath,
		DefaultTimeout: time.Duration(defaultTimeoutSec) * time.Second,
		MaxTimeout:     time.Duration(maxTimeoutSec) * time.Second,
		MaxOutputBytes: maxOutputBytes,
	}
	if cfg.Transport != TransportStdio && cfg.MCPRoute() == cfg.HealthRoute() {
		return Config{}, fmt.Errorf("invalid MCP_PATH %q: conflicts with health endpoint %q", cfg.MCPPath, cfg.HealthRoute())
	}

	return cfg, nil
}

func validateHTTPPath(envName, raw string) error {
	if strings.Contains(raw, "://") {
		return fmt.Errorf("invalid %s %q: must be a URL path (e.g. \"/path\"), not a full URL", envName, raw)
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || r < 0x20 || r == 0x7f {
			return fmt.Errorf("invalid %s %q: must not contain whitespace or control characters", envName, raw)
		}
		switch r {
		case '?', '#', '{', '}', '\\':
			return fmt.Errorf("invalid %s %q: must not contain %q", envName, raw, string(r))
		}
	}
	if _, err := url.PathUnescape(raw); err != nil {
		return fmt.Errorf("invalid %s %q: invalid percent-encoding: %w", envName, raw, err)
	}
	for _, seg := range strings.Split(raw, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("invalid %s %q: must not contain %q path segments", envName, raw, seg)
		}
	}
	return nil
}

func cleanBasePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	cleaned := path.Clean("/" + trimmed)
	if cleaned == "/" {
		return ""
	}
	return cleaned
}

func cleanRoutePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return path.Clean("/" + trimmed)
}

func joinHTTPPath(basePath, routePath string) string {
	base := cleanBasePath(basePath)
	route := cleanRoutePath(routePath)
	if route == "" {
		return base
	}
	if base == "" {
		return route
	}
	if route == "/" {
		return base
	}
	return base + route
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
