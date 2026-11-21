# mcp-bash

A Docker-hosted Bash MCP server written in Go using the official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`). It exposes a `bash` tool over Streamable HTTP (`/mcp`) by default, designed for running inside internal networks or Docker hosts such as Unraid.

## Features

- **Streamable HTTP by default**: Listens on `:8080` exposing `/mcp` and `/healthz`, with optional `sse` and `stdio` transports configurable via `MCP_TRANSPORT`.
- **Stateless command execution**: Every `bash` tool call runs in an independent `bash -c` process group with optional `cwd` and `timeout_seconds`, terminating the entire process group on timeout or cancellation to avoid orphaned child processes.
- **Debian 13 base image**: Built on `debian:13-slim` at runtime.
- **Unprivileged execution (`UID 99 / GID 100`)**: Runs as non-root `99:100` (`nobody:users`) with no `sudo`.
- **Pre-installed tools**: Includes `bash`, `python3` (`pip` and `venv`), `lightpanda` (headless browser with `LIGHTPANDA_DISABLE_TELEMETRY=true` by default; version controlled via build arg `LIGHTPANDA_VERSION=1.0.5`), `git`, `curl`, `fd` (symlinked from `fdfind`), `rg` (ripgrep), `wget`, `tar`, `xz`, `make`, `just`, `jq`, `gawk`, `file`, `tree`, `unzip`, `openssh-client`, etc.
- **Built-in agent instructions**: Server `Instructions` and tool descriptions inform connected AI agents about pre-installed tools and non-root constraints, directing any extra dependencies or binaries to `$HOME/.local` and `$HOME/.local/bin` (already on `PATH`, with `PIP_USER=1` and `PIP_BREAK_SYSTEM_PACKAGES=1` preconfigured).

## Quick Start

### Docker Compose

```bash
docker compose up -d --build
```

### Docker CLI

```bash
docker build -t mcp-bash:latest .
docker run -d \
  --name mcp-bash \
  --user 99:100 \
  --restart unless-stopped \
  -p 8080:8080 \
  mcp-bash:latest
```

Verify health:

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

## MCP Client Configuration

Point any Streamable HTTP-capable MCP client to:

```text
http://<your-host>:8080/mcp
```

Example JSON configuration:

```json
{
  "mcpServers": {
    "mcp-bash": {
      "type": "http",
      "url": "http://<your-host>:8080/mcp"
    }
  }
}
```

## Environment Variables

| Variable | Default | Description |
| --- | --- | --- |
| `MCP_TRANSPORT` | `streamable-http` | Transport mode: `streamable-http` (alias `http`), `sse`, or `stdio` |
| `MCP_ADDR` | `:8080` | HTTP listen address |
| `MCP_PATH` | `/mcp` (`/sse` for SSE) | MCP HTTP route path |
| `MCP_STATELESS` | `false` | Enable sessionless Streamable HTTP mode |
| `MCP_BASH_PATH` | `/bin/bash` | Path to the `bash` binary |
| `MCP_DEFAULT_TIMEOUT_SECONDS` | `60` | Default per-command timeout in seconds |
| `MCP_MAX_TIMEOUT_SECONDS` | `600` | Maximum allowed per-command timeout in seconds |
| `MCP_MAX_OUTPUT_BYTES` | `131072` | Maximum captured bytes per stream (`stdout` / `stderr`, default 128 KiB) |
| `LIGHTPANDA_DISABLE_TELEMETRY` | `true` | Disables Lightpanda telemetry in the container image |

## Dependency Installation Convention (`UID 99 / GID 100`)

- `$HOME` defaults to `/home/agent`, owned by `99:100`.
- To persist Python packages or CLI tools installed inside the container, mount a host volume to `/home/agent` with ownership `99:100`.
- The image prepends `/home/agent/.local/bin` to `PATH` and sets `PYTHONUSERBASE=/home/agent/.local`, `PIP_USER=1`, and `PIP_BREAK_SYSTEM_PACKAGES=1` so `pip install --user <pkg>` works out of the box.
