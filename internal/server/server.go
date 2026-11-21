package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elmhuangyu/mcp-bash/internal/config"
	"github.com/elmhuangyu/mcp-bash/internal/runner"
)

const ServerInstructions = `You are connected to mcp-bash, a Debian 13 Linux Docker environment running as an unprivileged user (UID 99 / GID 100, non-root, no sudo).

Pre-installed tools include: bash, python3 (with pip and venv), lightpanda (headless browser), git, gh (GitHub CLI), curl, wget, fd (fd-find), rg (ripgrep), tar, xz, make, just, jq, and standard GNU/CLI utilities.

Dependency installation rule:
- If you need to install extra dependencies, Python packages, or standalone binaries, install them under $HOME/.local and place executables in $HOME/.local/bin (which is already on PATH).
- Examples: ` + "`pip install --user <package>`" + `, placing binaries into ` + "`$HOME/.local/bin`" + `, or creating a virtualenv inside ` + "`$HOME`" + `.
- Do NOT attempt to install into system directories (/usr, /etc), or run apt-get or sudo.

Execution semantics:
- Each ` + "`bash`" + ` tool call runs in a fresh, independent ` + "`bash -c`" + ` process.
- By default the working directory is $HOME unless ` + "`cwd`" + ` is specified; ` + "`cd`" + ` and shell variables do not persist across calls.`

const BashToolDescription = `Execute a bash command inside the Debian 13 container via bash -c.

Environment notes:
- Runs as an unprivileged user (UID 99 / GID 100, no sudo).
- Pre-installed: bash, python3 (pip/venv), lightpanda, git, gh, curl, wget, fd, rg, tar, xz, make, just, jq.
- Install any extra dependencies or binaries under $HOME/.local / $HOME/.local/bin (already on PATH). Do not write to system paths or use apt-get/sudo.
- Each call runs in an independent bash process; pass cwd if you need a specific directory.`

// BashInput defines the parameters for the `bash` MCP tool.
type BashInput struct {
	Command        string `json:"command" jsonschema:"The bash command to execute via bash -c."`
	Cwd            string `json:"cwd,omitempty" jsonschema:"Optional working directory for this command. Defaults to $HOME."`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Optional timeout in seconds. Defaults to 60; maximum is 600."`
}

// NewMCPServer creates an MCP server with the `bash` tool registered.
func NewMCPServer(r *runner.Runner, logger *slog.Logger) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "mcp-bash",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: ServerInstructions,
		Logger:       logger,
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "bash",
		Description: BashToolDescription,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input BashInput) (*mcp.CallToolResult, any, error) {
		res, err := r.Run(ctx, runner.Request{
			Command:        input.Command,
			Cwd:            input.Cwd,
			TimeoutSeconds: input.TimeoutSeconds,
		})
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil, nil
		}

		return &mcp.CallToolResult{
			IsError: res.TimedOut || res.Canceled,
			Content: []mcp.Content{&mcp.TextContent{Text: runner.FormatResult(res)}},
		}, nil, nil
	})

	return s
}

// NewHTTPMux builds the HTTP handler exposing the configured health endpoint
// and MCP transport endpoint (including any configured base path).
func NewHTTPMux(cfg config.Config, mcpServer *mcp.Server, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(exactRoutePattern(cfg.HealthRoute()), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
		}
	})

	getServer := func(*http.Request) *mcp.Server {
		return mcpServer
	}

	mcpPattern := exactRoutePattern(cfg.MCPRoute())

	switch cfg.Transport {
	case config.TransportStreamableHTTP:
		handler := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
			Stateless:                  cfg.Stateless,
			DisableLocalhostProtection: true,
			Logger:                     logger,
		})
		mux.Handle(mcpPattern, handler)
	case config.TransportSSE:
		handler := mcp.NewSSEHandler(getServer, &mcp.SSEOptions{
			DisableLocalhostProtection: true,
		})
		mux.Handle(mcpPattern, handler)
	}

	return mux
}

func exactRoutePattern(route string) string {
	if route == "/" {
		return "/{$}"
	}
	return route
}
