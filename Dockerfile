FROM golang:1.27-trixie AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mcp-bash ./cmd/mcp-bash

FROM debian:13-slim

ARG DEBIAN_FRONTEND=noninteractive
ARG LIGHTPANDA_VERSION=1.0.5

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    curl \
    fd-find \
    file \
    gawk \
    git \
    jq \
    just \
    less \
    make \
    openssh-client \
    patch \
    procps \
    python3 \
    python3-pip \
    python3-venv \
    ripgrep \
    tar \
    tree \
    unzip \
    wget \
    xz-utils \
 && rm -rf /var/lib/apt/lists/* \
 && ln -sf /usr/bin/fdfind /usr/local/bin/fd \
 && ln -sf /usr/bin/python3 /usr/local/bin/python

RUN arch="$(dpkg --print-architecture)" \
 && case "${arch}" in \
      amd64) lp_arch="x86_64" ;; \
      arm64) lp_arch="aarch64" ;; \
      *) echo "Unsupported architecture for lightpanda: ${arch}" >&2; exit 1 ;; \
    esac \
 && curl -fsSL --retry 3 --retry-delay 2 \
      -o /usr/local/bin/lightpanda \
      "https://github.com/lightpanda-io/browser/releases/download/${LIGHTPANDA_VERSION}/lightpanda-${lp_arch}-linux" \
 && chmod 0755 /usr/local/bin/lightpanda \
 && /usr/local/bin/lightpanda version

RUN (getent group 100 >/dev/null || groupadd -g 100 users) \
 && (getent passwd 99 >/dev/null || useradd --uid 99 --gid 100 --home-dir /home/agent --create-home --shell /bin/bash agent) \
 && mkdir -p /home/agent/.local/bin /home/agent/.cache /home/agent/.config /home/agent/.local/share \
 && chown -R 99:100 /home/agent

COPY --from=builder --chown=root:root --chmod=0755 /out/mcp-bash /usr/local/bin/mcp-bash

ENV HOME=/home/agent \
    PATH=/home/agent/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    PYTHONUSERBASE=/home/agent/.local \
    PIP_USER=1 \
    PIP_BREAK_SYSTEM_PACKAGES=1 \
    XDG_CACHE_HOME=/home/agent/.cache \
    XDG_CONFIG_HOME=/home/agent/.config \
    XDG_DATA_HOME=/home/agent/.local/share \
    LIGHTPANDA_DISABLE_TELEMETRY=true \
    MCP_TRANSPORT=streamable-http \
    MCP_ADDR=:8080

USER 99:100
WORKDIR /home/agent
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/mcp-bash"]
