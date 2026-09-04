English | [Korean](monitoring.kr.md)

# Analyze experiments with Prometheus and Grafana

Run `docker compose up -d --build` to start the Controller, two Agents, Prometheus, and Grafana together. The data source and **KP2PLab Experiment Analysis** dashboard are provisioned automatically.

The dashboard is written in English, and Compose/Swarm set `GF_USERS_DEFAULT_LANGUAGE=en-US` for the Grafana UI; user or organization preferences can override that UI default. See [Grafana language preferences](https://grafana.com/docs/grafana/latest/administration/organization-preferences/#change-grafana-language).

Grafana initializes its SQLite database on first startup, which may take several minutes depending on disk performance. Follow initialization with `docker compose logs -f grafana`. Subsequent starts reuse the existing database.

The local SQLite database uses WAL mode. Both the database and WAL files are retained in the same Grafana named volume. See the [Grafana database settings](https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/#wal).

| Interface | Default address |
|---|---|
| KPL Control Room | http://localhost:8080 |
| Grafana experiment analysis | http://localhost:3000/d/kpl-experiments |
| Prometheus queries and target status | http://localhost:9090 |
| Controller metrics endpoint | http://localhost:8080/metrics |

Grafana allows anonymous, read-only access for local analysis. Administrator credentials come from `GRAFANA_ADMIN_USER` and `GRAFANA_ADMIN_PASSWORD` in `.env`. In this workspace, a random administrator password was generated and saved in the Git-ignored `.env` file. For a new installation, configure these values using `.env.example`. Without `.env`, the initial administrator credentials are Grafana's default `admin`/`admin`. Changing environment variables alone does not update the password in an existing Grafana data volume.

Prometheus and Grafana ports are published only on `127.0.0.1`. Set `PROMETHEUS_PORT` and `GRAFANA_PORT` in `.env` to change their default ports. Set `GRAFANA_ANONYMOUS_ENABLED=false` to disable anonymous viewing.

## Run and analyze an experiment

1. Use **Run experiment** in the Control Room to run [`examples/monitoring.yaml`](../examples/monitoring.yaml). This small experiment includes both envelope and raw publications.
2. Select **Run** (`run_id`), **Agent**, and **Topic** in Grafana. Selecting multiple runs aggregates their traffic and latency samples; network configuration time series identify each run in their legends.
3. After an experiment finishes, set the time range to its execution window to view the recorded series. The default refresh interval is 5 seconds.

Prometheus scrapes `/metrics` on the Controller and both Agents every 5 seconds. It does not scrape Peers directly or add exporters to individual Peer containers. The Controller aggregates the existing telemetry stream, so telemetry loss under `scope: all` also affects these metrics. Monitoring services use a separate Docker network; Peers receive no additional networks or permissions.

## Metric definitions

| Metric | Meaning |
|---|---|
| `kpl_events_total` | Cumulative events received by the Controller, labeled by `run_id`, `agent_id`, `event_type`, and `topic` |
| `kpl_message_bytes_total` | Published/delivered PubSub data bytes, excluding libp2p framing and TCP/IP headers |
| `kpl_propagation_latency_seconds` | Histogram of valid envelope delivery latency; excludes raw messages, negative values, and unmeasured values |
| `kpl_operation_failures_total` | Publish/leave failures recorded under `onError: continue` |
| `kpl_telemetry_dropped_events_total` | Events that Peers report dropping because their telemetry queues were full |
| `kpl_nodes` | Peer counts by experiment, Agent, group, role, type, and state |
| `kpl_agent_*` | Agent online status, capacity, and latest heartbeat observed by the Controller |
| `kpl_experiment_*` | Experiment state, phase, and job state |
| `kpl_network_configured_*` | Effective configurations of starting/ready Peers, aggregated by group; delay includes mean/min/max, while jitter/loss report the mean |
| `kpl_local_*` | Each Agent's local Peer states, capacity, pending cleanup, and telemetry queue length |
| `go_*`, `process_*` | Runtime, CPU, and memory metrics for the scraped Controller/Agent processes; these do not represent total Peer container resource usage |

`kpl_network_configured_loss_ratio` is the configured packet loss ratio, not an observed loss rate. Configured delay is also distinct from measured RTT. The `graft`/`prune`/`remove_peer` events help analyze PubSub mesh changes; they are neither a TCP connection graph nor a complete mesh snapshot.

Deliveries (`deliver`) include the publishing Peer's own local delivery. The delivery-to-publication ratio therefore cannot be interpreted as packet loss, and the latency histogram includes local delivery samples. TCP retransmissions can also turn packet loss into increased latency rather than message loss.

Cumulative counters are independent of the web interface's 300-event recent buffer. They reset when the Controller process restarts, and historical `events.jsonl` files are not replayed automatically. Time series already stored in Prometheus remain available, and `rate`/`increase` handle observed counter resets. They cannot recover events that disappeared before a scrape or telemetry that failed to arrive. Because `increase` estimates interval growth from scrape samples, it does not always exactly match integer cumulative event counts.

Raw deliveries contribute to delivery counts and bytes but not to the latency histogram. Do not interpret intervals without latency samples as 0 ms. Clock synchronization affects propagation latency measurements when Peers run on different hosts.

## Execution validation

On 2026-09-04, `examples/monitoring.yaml` was executed and the following results were compared with the original `events.jsonl`. The run ID was `run-20260904T024349Z-345a`. All test Peer containers were removed after the experiment finished.

| Check | Result |
|---|---|
| Publications / deliveries | 66 / 264, matching Prometheus and the original log |
| Cumulative events | 583 retained; the web recent-event buffer held 300 |
| Latency samples | Included only 240 envelope deliveries; excluded 24 raw deliveries |
| Worker network configuration | Delay 50 ms, jitter 5 ms, loss 1% |
| Telemetry queue drops | 0 |

These figures describe that specific validation run. Duplicate deliveries, mesh event counts, and measured latency vary with the execution environment.

Heartbeat processing also initializes zero baselines for `add_peer` and `remove_peer`. This reduces the risk of missing the initial increase when a series is first created by a Peer departure event. A subsequent run after this change (`run-20260904T025651Z-ee92`) again recorded 66 publications, 264 deliveries, and 240 latency samples; all three Peer departures were also reflected in the interval increase. In very short experiments, events occurring before the first scrape can still be missing from the estimated increase. Compare cumulative counters or original logs when exact counts are required.

## Retention and operation

Prometheus time series are stored in the `prometheus-data` named volume, and Grafana settings in `grafana-data`. Both survive ordinary container recreation. Prometheus retention is set to 15 days or 5 GB, whichever limit is reached first. The 5 GB setting is not a hard ceiling on disk usage: WAL, head data, and compaction require additional space. See the [Prometheus storage documentation](https://prometheus.io/docs/prometheus/latest/storage/).

Edits to dashboard JSON files in `monitoring/grafana/dashboards` are applied at 30-second intervals. The provisioned originals are managed as files. To save a separate dashboard, sign in as an administrator and work with a copy. See the [Grafana provisioning documentation](https://grafana.com/docs/grafana/latest/administration/provisioning/).

```bash
# Check scrape target status.
curl http://localhost:9090/api/v1/targets

# Validate the Prometheus configuration.
docker compose exec prometheus promtool check config /etc/prometheus/prometheus.yml

# Check service status and logs.
docker compose ps
docker compose logs --tail=100 prometheus grafana
```

When distributing Agents across hosts, change the Agent targets in `monitoring/prometheus/prometheus.yml` to reachable `/metrics` addresses. Local Compose network names do not discover remote hosts.

Image versions are pinned to Prometheus `v3.13.2` and Grafana `13.2.1`. When upgrading, consult the official [Prometheus downloads](https://prometheus.io/download/) and [Grafana Docker installation guide](https://grafana.com/docs/grafana/latest/setup-grafana/installation/docker/), then revalidate the configuration and dashboards.
