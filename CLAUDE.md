# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**leader-elector** is a small Go binary that runs as a sidecar (typically an `initContainer`) in a Kubernetes Pod to perform leader election among replicas of a Deployment. It wraps `k8s.io/client-go`'s `leaderelection` package, uses a `Lease` object (`coordination.k8s.io`) as the lock, and exposes the current leader's identity over a tiny HTTP endpoint so other containers in the same Pod can query it.

## Tech Stack

- Go 1.26 (see `go.mod`; the Dockerfile builds with `golang:1.26-alpine`)
- `k8s.io/client-go` (`leaderelection`, `resourcelock.LeaseLock`) for the election itself — always uses `rest.InClusterConfig()`, so it only runs inside a cluster
- `github.com/alexflint/go-arg` for CLI flag / env var parsing
- `k8s.io/klog/v2` for logging
- No test files, no Makefile, no linter config, no CI test job — the only CI workflow builds and publishes the Docker image

## Architecture

The whole program is two files:

- `main.go` — parses CLI args/env vars into a package-level `args` struct, starts a minimal HTTP server (`net/http`, no framework) that serves the current leader as JSON (`{"name": "..."}`) on `/`, wires up an `os.Interrupt`/`SIGTERM` handler that cancels a `context.Context` to trigger graceful leader step-down, then calls `Run` (blocking) and shuts the HTTP server down afterward.
- `election.go` — `Run(ctx, cfg Config)` builds an in-cluster Kubernetes client, constructs a `resourcelock.LeaseLock` scoped to `cfg.LockName`/`cfg.LockNamespace` with `Identity` set to `$HOSTNAME` (the Pod name), and calls `leaderelection.RunOrDie` with that lock. `cfg.Callback` (set to a closure in `main.go` that updates the package-level `leader` var) fires on `OnStartedLeading` and `OnNewLeader`, so the HTTP handler always reflects the most recently observed leader, not just "am I the leader."

Configuration is entirely via CLI flags or environment variables (`go-arg` struct tags in `main.go`):

| Flag | Env var | Default | Purpose |
|---|---|---|---|
| `--election` | `ELECTION_NAME` | `default` | Name of the Lease object |
| (positional `Namespace`) | `ELECTION_NAMESPACE` | `default` | Namespace of the Lease |
| `--renew-deadline` | `ELECTION_RENEW_DEADLINE` | `10s` | How long the leader retries renewing before giving up |
| `--retry-period` | `ELECTION_RETRY_PERIOD` | `2s` | Delay between election retries |
| `--lease-duration` | `ELECTION_LEASE_DURATION` | `15s` | How long non-leaders wait before attempting to acquire an unrenewed lease |
| `--port` | `ELECTION_PORT` | `4040` | Port for the leader-query HTTP endpoint |

The `example/` directory shows the intended deployment pattern: `leader-elector` runs as an `initContainer` sidecar in a Deployment, and a `ServiceAccount`/`Role`/`RoleBinding` (`example/rbac.yaml`) grant it `get`/`create`/`update` on `leases.coordination.k8s.io` in its namespace. Other containers in the same Pod query `http://localhost:<port>` to learn who currently holds leadership.

## Common Commands

```bash
# Build
go build -o elector .

# Run locally (requires in-cluster config — will fail with
# "unable to get cluster config" unless run inside a real Pod,
# e.g. via `kubectl exec` or as a sidecar)
go run .

# Tidy/update dependencies
go mod tidy

# Build the release Docker image (distroless, multi-arch via Dockerfile)
docker build -t leader-elector .
```

There are no test files, linter configuration, or Makefile in this repo. The only GitHub Actions workflow (`.github/workflows/publish.yml`) builds and pushes a multi-arch (`linux/amd64,linux/arm64`) Docker image to Docker Hub (`localopsroot/leader-elector`) when a `v*.*.*` tag is pushed — it does not run tests.
