# Development Guide

English | [Korean](development.kr.md)

The canonical Go module path is `github.com/k-p2p-lab/v3`, and [`go.mod`](../go.mod) requires Go 1.25 or later. This guide covers the v3 development workflow; the [Hub](https://github.com/k-p2p-lab/hub) owns project goals and conceptual design.

## Build and Validate on Linux

Run the following commands from the repository root in Linux:

```sh
go build -o bin/kpl ./cmd/kpl
go test -buildvcs=false ./... -timeout 120s
sh scripts/test-swarm-agent.sh
sh scripts/test-check-swarm.sh
sh scripts/test-swarm-config.sh
sh scripts/test-swarm.sh
```

Validate a scenario without running it:

```sh
./bin/kpl validate --scenario examples/smoke.yaml
```

## Run development experiments on Swarm

Build and publish the updated image, then deploy it with the [Swarm workflow](swarm.md). A single Linux node can host a development Swarm: use `KPL_MIN_AGENTS=1` and `deploy --all`. Agents require an active Swarm node and an explicitly configured image and attachable Peer overlay. Run the distributed smoke scenario on at least two Agent nodes when checking cross-host placement and connectivity.

## Container and Browser Regression Checks

With an already available Linux Docker daemon, run the Docker test target without installing Go on the host:

```sh
make test-linux
# Equivalent when make is unavailable:
docker build --target test -t kpl-v3:test .
```

The [`Dockerfile`](../Dockerfile) test target runs four Swarm shell regression scripts and the Go package tests. Its shell tests simulate Docker responses. The two-container network impairment test is opt-in and skips in an ordinary test build; this target does not establish actual Swarm placement, cross-host connectivity, or kernel shaping behavior.

The browser logic also has Node.js regression tests, which are separate from the Docker test target. With Node.js installed, run:

```sh
node --test internal/webui/*_test.cjs
```

## Windows Workspace Rules

Use Windows for editing and static inspection. Do not run native Windows `go test`, `go test -c`, generated test executables, or helpers such as `make test` that invoke them. Do not launch commands that require an interactive firewall, security, or elevation dialog. Use the Linux Docker test target above or an existing non-interactive Linux host for executable validation. If neither is available, report static inspection separately from runtime validation.

The runtime image uses `CGO_ENABLED=0`. Shell scripts, Dockerfiles, and Makefiles retain LF endings through [`.gitattributes`](../.gitattributes). The runtime image contains the built application and runtime utilities, not Go or Node.js development tools.
