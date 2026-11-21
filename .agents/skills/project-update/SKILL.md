---
name: project-update
description: >-
  Runbook for updating Go dependencies, toolchain, Docker base images, bundled
  binaries (lightpanda, gh), and GitHub Actions in the mcp-bash repository. Use
  when the user asks to update project dependencies, upgrade Go/Docker/CI
  versions, or perform a routine dependency audit and bump.
---

# Project Update Runbook

Use this workflow when updating dependencies, runtime toolchains, Docker images,
bundled CLI binaries, or GitHub Actions in `mcp-bash`.

Never edit `go.sum` by hand.

## 1. Go Dependencies

1. **Check current & outdated direct dependencies**:
   ```bash
   go list -u -m -f '{{if and (not .Indirect) .Update}}{{.Path}}: {{.Version}} -> {{.Update.Version}}{{end}}' all
   ```
2. **Update Go dependencies**:
   - Routine minor/patch/compatible updates:
     ```bash
     just update-deps
     ```
     (`just update-deps` runs `go get -u -t ./...`, then runs `just tidy`.)
   - For targeted or major version bumps, run `go get <pkg>@<version>`, then run `just tidy`.
3. **Check for Go vulnerabilities**:
   ```bash
   go run golang.org/x/vuln/cmd/govulncheck@latest ./...
   ```
4. **Check Go toolchain version**:
   - Check the `go X.Y` directive in `go.mod`.
   - Keep the Go version aligned with `Dockerfile` (`FROM golang:<ver>-trixie AS builder`).
   - `.github/workflows/test-on-push.yml` uses `go-version-file: go.mod`, so CI follows `go.mod` automatically.

## 2. Docker Dependencies, Base Images & Bundled Binaries

Inspect and update `Dockerfile`:

- `FROM golang:<ver>-trixie AS builder`
- `FROM debian:<ver>-slim`
- `ARG LIGHTPANDA_VERSION=<ver>` (`lightpanda-io/browser`)
- `ARG GH_VERSION=<ver>` (`cli/cli`)

Checklist:
- Verify the latest stable tags for `golang:*-trixie` and `debian:*-slim`.
- Ensure the Go version matches across `Dockerfile` and `go.mod`.
- Check the latest releases for bundled binaries with `gh`:
  ```bash
  for repo in lightpanda-io/browser cli/cli; do
    tag=$(gh api "repos/$repo/releases/latest" --jq '.tag_name')
    echo "$repo: $tag"
  done
  ```
  Note: `GH_VERSION` strips the leading `v` (e.g., `2.105.0` for `v2.105.0`), while `LIGHTPANDA_VERSION` matches the tag directly (e.g., `1.0.5`).
- If `LIGHTPANDA_VERSION` or `GH_VERSION` changes, update `README.md` to keep documented versions in sync.
- Build the image locally to verify:
  ```bash
  just build-image
  ```

## 3. GitHub Actions & CI-Installed Dependencies

Inspect `.github/workflows/test-on-push.yml`, `.github/workflows/push-image.yml`, and `.github/dependabot.yml`.

1. **CI-installed tool versions** (`.github/workflows/test-on-push.yml` `env:`):
   - `GOIMPORTS_VERSION`
   - `GOLANGCI_LINT_VERSION`
2. **GitHub Action versions**:
   - `.github/workflows/test-on-push.yml`:
     - `actions/checkout`
     - `actions/setup-go`
   - `.github/workflows/push-image.yml`:
     - `actions/checkout`
     - `docker/login-action`
     - `docker/metadata-action`
     - `docker/build-push-action`
   - Check latest action releases with `gh`:
     ```bash
     for repo in \
       actions/checkout \
       actions/setup-go \
       docker/login-action \
       docker/metadata-action \
       docker/build-push-action
     do
       tag=$(gh api "repos/$repo/releases/latest" --jq '.tag_name')
       echo "$repo: $tag"
     done
     ```

## 4. Verification

Run the full local suite after all updates:

```bash
just fmt lint build test
```

If `Dockerfile` was modified, also run:

```bash
just build-image
```

Check `git status` and `git diff` to verify only expected files changed, and
commit using conventional commits (e.g., `build(deps): ...` or `ci: ...`).
