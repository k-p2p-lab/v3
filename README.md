# K-P2PLab v3

English | [Korean](README.kr.md)

K-P2PLab v3 runs reproducible libp2p Kademlia and PubSub experiments across one or more Linux hosts. A Controller schedules scenarios, one Agent manages each host, and every Peer runs in its own Docker container and network namespace. The web Control Room and the bundled Prometheus/Grafana stack expose topology, churn, propagation, and saved results.

English is the default language for code, the UI, and documentation. Korean documentation is maintained in matching `.kr.md` files.

## Core features

- Version 2 YAML scenarios with joins, leaves, readiness barriers, publishing, repeated phases, background jobs, and seeded distributions
- Isolated Peer containers with per-Peer delay, jitter, loss, duplication, corruption, reordering, and bandwidth controls
- Single-host Docker Compose and multi-server Docker Swarm deployment with capacity-aware Peer placement
- Live Kademlia, GossipSub GRAFT, and transport topology with Agent sectors and topic filters
- Churn-aware delivery, latency, duplicate, coverage, and observation-quality metrics
- Reusable scenario library plus repeat runs, persisted results, ZIP export, and deletion

## Prerequisites

- Linux with rootful Docker Engine and Docker Compose v2; the supplied deployment does not support userns-remap
- `NET_ADMIN` and the kernel `sch_prio`, `sch_netem`, `cls_u32`, and optional `sch_tbf` modules for network conditions
- Go 1.24 or later only for development outside Docker
- For Swarm: an active manager and an image registry reachable and trusted by every selected node

See the [Linux deployment guide](docs/linux-deployment.md) for permissions, remote access, storage, and safe shutdown.

## Local quick start

```sh
test -f .env || cp .env.example .env
# Set KPL_API_TOKEN and GRAFANA_ADMIN_PASSWORD in .env.
docker compose build controller
sh scripts/check-linux.sh
docker compose up -d --no-build
```

Open the [Control Room](http://localhost:8080), [Grafana](http://localhost:3000/d/kpl-experiments), or [Prometheus](http://localhost:9090). Run the default scenario from the Control Room. Stop cleanly with `make stop`.

## Swarm quick start

Run these commands from the repository on the active manager. Replace the image reference with a registry accessible to every node.

```sh
sh scripts/swarm.sh init KPL_IMAGE=registry.example.com/kpl-v3:v3 KPL_AGENT_CAPACITY=20 KPL_MIN_AGENTS=2
sh scripts/swarm.sh publish
sh scripts/swarm.sh deploy --workers
sh scripts/swarm.sh status
sh scripts/swarm.sh access
sh scripts/swarm.sh credentials
sh scripts/swarm.sh scenario
```

Use `--all` instead of `--workers` when the manager must also run an Agent. Open the Controller URL printed by `access`, paste the scenario printed by `scenario`, and use the API token printed by `credentials`. The helper resolves and pins the current image digest during deployment, so tag updates do not require manual SHA edits. Read the [complete Swarm workflow](docs/swarm.md) before operating or removing a production cluster.

## Documentation

| Guide | Contents |
|---|---|
| [Linux deployment](docs/linux-deployment.md) | Single-host preparation, permissions, storage, remote access, and shutdown |
| [Swarm deployment](docs/swarm.md) | Registry setup, node selection, deployment, updates, scaling, and removal |
| [Scenario configuration](docs/scenario-reference.md) | YAML actions, profiles, protocol controls, distributions, and network conditions |
| [Scenario library](docs/scenario-library.md) | Save, name, load, update, and delete reusable scenarios |
| [REST API](docs/api.md) | Controller endpoints, authentication, results, and internal cleanup API |
| [Development](docs/development.md) | Go build, validation, tests, and the local process runtime |
| [Experiment metrics](docs/experiment-metrics.md) | Churn-aware delivery denominator, latency, duplicates, and limitations |
| [Monitoring and results](docs/monitoring.md) | Prometheus, Grafana, ZIP contents, retained data, and deletion |
| [Topology](docs/topology.md) | Agent sectors, graph layers, topic filters, and controls |
| [Swarm churn and publish](docs/swarm-churn-publish.md) | Multi-server continuous-churn experiment walkthrough |
| [v2 reproduction](docs/v2-reproduction.md) | Compatibility mapping and intentional differences from K-P2PLab v2 |
