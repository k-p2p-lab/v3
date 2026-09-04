# Linux Deployment Guide

English | [Korean](linux-deployment.kr.md)

The deployment target is Linux Docker Engine. Windows can serve as a development host, but Peer execution, network control, and shutdown verification run in Linux containers. The supplied Compose configuration targets **one Linux server with rootful Docker, no userns-remap, and an IPv4 user-defined bridge**.

If your servers are already joined to a Swarm, follow the [manager workflow from preparation through experiment downloads and removal](swarm.md) instead. Its `scripts/swarm.sh` commands manage the shared deployment; the Compose commands below are for a single server.

## Prepare and Start the Server

Install Docker Engine and the Compose plugin, then run the following commands with the same user and Docker context. Docker 28 or later is recommended. Earlier versions have a limitation that can make ports published to localhost accessible from the same L2 network. [Docker port publishing](https://docs.docker.com/engine/network/port-publishing/)

```sh
# Run from the repository directory. Preserve an existing .env file.
test -f .env || cp .env.example .env
chmod 600 .env
# Set KPL_API_TOKEN and GRAFANA_ADMIN_PASSWORD in .env.

docker compose build controller
sh scripts/check-linux.sh
docker compose config --quiet
docker compose up -d --no-build
docker compose ps
```

The preflight check verifies the runtime image, Docker access, Compose, and installation of prio/netem/u32/TBF rules on the actual kernel. It requires `timeout` and `setsid`. It creates a separate container and removes it on exit; it does not apply tc rules to host interfaces or ongoing experiments. If cancellation arrives during container creation, the check first collects the creation result. If Docker delays or another issue prevent cleanup from being confirmed within the time limit, it prints the exact container name to inspect manually. Set `KPL_DOCKER_IMAGE` to check a different image name.

Network shaping requires `NET_ADMIN` and kernel support for `sch_prio`, `sch_netem`, and `cls_u32`, plus `sch_tbf` when using TBF. These modules may be built into the kernel, so `lsmod` output alone does not establish support. If installation fails, check the target server's kernel configuration and available modules. Peers are not granted permission to load modules into the host kernel.

## Access the UI Remotely

Controller port 8080, Grafana port 3000, and Prometheus port 9090 are published on the server's `127.0.0.1` by default. An SSH tunnel from your development machine lets you use the same localhost URLs.

```sh
ssh -N -L 8080:127.0.0.1:8080 -L 3000:127.0.0.1:3000 -L 9090:127.0.0.1:9090 user@linux-server
```

To publish the Controller directly on a management-network address, set `KPL_BIND_ADDRESS` and `KPL_HTTP_PORT` in `.env`. `KPL_API_TOKEN` protects mutation APIs; it does not authenticate GET requests. See the [monitoring guide](monitoring.md) for the Grafana administrator password and anonymous read-only access settings.

## Shutdown and Server Restart

**Stop the Controller first**, then bring down the remaining services, so the Controller can cancel active experiments and request Peer cleanup from Agents.

```sh
docker compose stop controller
docker compose down
# If make is installed, make stop uses the same order.
```

On SIGTERM, the Controller stops accepting new experiments and waits for HTTP connections, experiment jobs, Peer cleanup, and the final run-state save. The default `jobShutdownTimeout: 3m` applies separately to job shutdown and Peer cleanup, so Compose gives the Controller a `7m` stop grace period and the Agent `3m`. Increase the Controller's grace period as well when a scenario uses a longer `jobShutdownTimeout`. If an external systemd unit manages Compose, its stop timeout must not cut these grace periods short.

Running only `docker compose down` can stop the Agent first because services stop in reverse dependency order. Agents also clean up their own Peers, but use the sequence above to preserve the Controller's final state record as well. After forced termination or power loss, Agent startup reclaims leftover managed containers with the same Agent ID and network. It does not automatically resume the previous experiment.

The Controller fails at startup if its data directory is not writable. Run metadata is written to a temporary file, closed, and then replaced by a rename on the same filesystem, preventing Linux readers from seeing partially written JSON. Writes are not forcibly synchronized: an fsync at every phase would delay experiment timing and telemetry processing. The event log uses append writes. Neither file guarantees that every record survives a power loss on disk.

## Docker Permissions and Storage

Only the Agent uses the Docker socket, and it runs as `0:0` inside its container. Peers receive no Docker socket, host-directory mounts, or published host ports. Peers also run as `0:0` to access their configuration file, but all default capabilities are dropped and `no-new-privileges` is enabled. `NET_ADMIN` is added only when network conditions are configured. An Agent with Docker socket access can manage Docker containers on that server.

The default socket path is `/var/run/docker.sock`. Rootless Docker uses a different socket location and network namespace and does not support overlay networks, so it is outside the supported baseline for the supplied Compose configuration. userns-remap also requires separate configuration for Agent socket access. Do not work around this by making the socket writable by everyone or running Peers as privileged containers. [Docker Rootless limitations](https://docs.docker.com/engine/security/rootless/troubleshoot/)

Experiment records, Prometheus data, and Grafana data are stored in named volumes. `docker compose down` preserves them, but `down -v` deletes them and should not be used for routine shutdown. If you replace data volumes with bind mounts, check write access for the container's UID/GID. With a remote Docker daemon, bind-mount source paths are resolved on the daemon host, so keeping this repository and running these commands on the deployment server is recommended. [Docker bind mounts](https://docs.docker.com/engine/storage/bind-mounts/)

On servers with SELinux enforcing, check read access and container file labels for `monitoring/`. If needed, apply `:ro,z` only to this project's monitoring bind mounts. Do not relabel `/var/run/docker.sock` or system directories; permit the Agent's socket access according to the server's container policy. The current validation environment does not use SELinux, so this policy must be checked on the target server. No configuration to disable AppArmor or SELinux globally is provided.

## Scope of Network Experiments

Peers communicate over IPv4 container addresses on TCP 20000; the control API uses TCP 18000. `scope: p2p` shapes only outbound TCP 20000 traffic, while `scope: all` shapes outbound traffic including control and telemetry. IPv6-only experiments, host networking, and remote Agent setups without direct access to container IPs are outside the supported baseline.

The maximum accepted `jitter` is `2.147483647s`, preventing the kernel from silently reducing larger values. The compatibility path for netem delay attributes was reviewed for Linux 5.15 and 6.8; the netem seed option added in newer kernels is not sent. Use the preflight check on your server to verify actual support. [Linux 6.8 netem implementation](https://github.com/torvalds/linux/blob/v6.8/net/sched/sch_netem.c)

For multiple physical hosts, separately prepare cross-host connectivity, such as an attachable overlay, and ensure all Agents and Peers can reach one another on the same network. The default Compose bridge applies to only one Docker host. Make the same runtime image available on every host and use unique Agent IDs. Synchronize host clocks with chrony/NTP or an equivalent service when comparing latency measurements. [Docker overlay requirements](https://docs.docker.com/engine/network/drivers/overlay/)

## Linux Validation

For a Swarm deployment across multiple servers, start with the [dedicated deployment and node distribution guide](swarm.md). Scaling up the replicas of the single-host Compose configuration is not supported.

```sh
# Run the full Go test suite on Linux without installing Go on the host.
docker build --target test -t kpl-v3:test .
# Alternatively, run make test-linux.
```

The runtime image is built with `CGO_ENABLED=0`; the final Alpine image does not include Go build tools. `.gitattributes` preserves LF line endings for shell scripts, Dockerfiles, and Makefiles. Scripts are invoked as `sh scripts/check-linux.sh`, so they do not depend on executable bits in a Windows checkout.

The current validation used Docker Desktop's Linux kernel `6.18.33.2-microsoft-standard-WSL2` on amd64. This exercises real Linux syscalls, namespaces, and tc, but does not replace testing the target server's distribution, kernel, SELinux policy, cross-host overlay, or performance at scale. Run the preflight check and `examples/monitoring.yaml` again on the deployment server to verify configuration and collected results.

### Validation Results: 2026-09-04

| Item | Result |
|---|---|
| Full Linux Go test suite | All packages passed, including concurrent JSON reads and Controller shutdown waiting tests |
| ARM64 | Cross-build passed with `GOOS=linux GOARCH=arm64 CGO_ENABLED=0`; ELF AArch64 verified. Execution on ARM64 hardware was not tested |
| Kernel preflight | Ran in an official Linux Docker CLI/Compose container; prio/netem/u32/TBF installation and cleanup passed |
| Actual TCP conditions | Verified P2P 100% loss with control-port traffic preserved, 100ms delay, TBF 1Mbit shaping, and control-traffic loss under `scope: all` using two separate containers |
| Permissions | tc failed without NET_ADMIN. A read-only Controller data directory caused a startup error and exit 1 |
| SIGTERM | Cleaned up the three ready Peers in active run `run-20260904T031927Z-0b3e`, then exited 0 after about 16.72 seconds; no Peer containers remained |
| Final state | Confirmed `canceled` and `finishedAt` in the stopped Controller's `experiment.json`, then restarted the services |

The list of 600 container IDs in unit tests is a simulated response for testing large inventory queries and cleanup logic. It is not a load-test result from 600 real Peers.
