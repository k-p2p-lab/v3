English | [Korean](monitoring.kr.md)

# Analyze experiments with Prometheus and Grafana

Run `docker compose up -d --build` to start the Controller, two Agents, Prometheus, and Grafana together. The data source and **KP2PLab Experiment Analysis** dashboard are provisioned automatically.

The dashboard is written in English, and Compose/Swarm set `GF_USERS_DEFAULT_LANGUAGE=en-US` for the Grafana UI; user or organization preferences can override that UI default. See [Grafana language preferences](https://grafana.com/docs/grafana/latest/administration/organization-preferences/#change-grafana-language).

Grafana initializes its SQLite database on first startup, which may take several minutes depending on disk performance. Follow initialization with `docker compose logs -f grafana`. Subsequent starts reuse the existing database.

The local SQLite database uses WAL mode. Both the database and WAL files are retained in the same Grafana named volume. See the [Grafana database settings](https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/#wal).

| Interface | Default address |
|---|---|
| KPL Dashboard | http://localhost:8080 |
| Grafana experiment analysis | http://localhost:3000/d/kpl-experiments |
| Prometheus queries and target status | http://localhost:9090 |
| Controller metrics endpoint | http://localhost:8080/metrics |

Grafana allows anonymous, read-only access for local analysis. Administrator credentials come from `GRAFANA_ADMIN_USER` and `GRAFANA_ADMIN_PASSWORD` in `.env`. In this workspace, a random administrator password was generated and saved in the Git-ignored `.env` file. For a new installation, configure these values using `.env.example`. Without `.env`, the initial administrator credentials are Grafana's default `admin`/`admin`. Changing environment variables alone does not update the password in an existing Grafana data volume.

Docker Compose publishes the Prometheus and Grafana ports only on `127.0.0.1`; Swarm publishes them on the control node. Swarm also publishes each Agent's dedicated metrics listener on its own node at `KPL_AGENT_METRICS_PORT` (default `9091`). Set `PROMETHEUS_PORT` and `GRAFANA_PORT` in Compose `.env`, or use `scripts/swarm.sh configure` for Swarm ports. Set `GRAFANA_ANONYMOUS_ENABLED=false` to disable anonymous viewing in Compose.

The Dashboard header links to Prometheus and Grafana in new tabs. It combines the current Dashboard host with the configured published ports, so the links follow either an SSH tunnel or the Swarm control-node address used to open the Dashboard.

## Run and analyze an experiment

1. Use **Run experiment** in the Dashboard to run [`examples/monitoring.yaml`](../examples/monitoring.yaml). This small experiment includes both envelope and raw publications.
   Set **Runs** beside **Run** to 1–100 for sequential iterations. Each gets a separate result; failure or **Stop batch** cancels the remainder. See [repetition and metric definitions](experiment-metrics.md).
2. Select **Run** (`run_id`), **Agent**, and **Topic** in Grafana. Selecting multiple runs aggregates their traffic and latency samples; network configuration time series identify each run in their legends.
3. After an experiment finishes, set the time range to its execution window to view the recorded series. The default refresh interval is 5 seconds.

Prometheus scrapes `/metrics` on the Controller and Agents every 5 seconds. Compose uses its two static Agent service names. In Swarm, the Controller returns the metrics URLs of registered Agents through HTTP service discovery; Prometheus then reaches each Agent's host-mode port directly instead of a service VIP. It does not scrape Peers directly or add exporters to individual Peer containers. The Controller aggregates the existing Peer telemetry stream, so telemetry loss under `scope: all` still affects Controller-derived Peer metrics. Monitoring services use a separate Docker network; Peers receive no additional networks or permissions.

## Download experiment results

In the Dashboard, choose **Download results** on an experiment or in **Saved results**. The saved list reads the Controller's data directory, so results remain accessible after a Controller restart. Use **Refresh** after restoring files from a backup. Running experiments offer **Download snapshot**. For inactive rows, the UI measures an unknown ZIP size in the background and then displays it; running and queued rows omit the size because their files change frequently. A later event or an elapsed delivery deadline can change the next snapshot and its size.

Each ZIP contains:

| File | Contents |
|---|---|
| `scenario.yaml` | Exact scenario submitted for the run |
| `experiment.json` | Original saved experiment metadata, state, seed, and job counters |
| `events.jsonl` | All event records saved at the export boundary; one JSON object per line, or an empty file when no events have been recorded |
| `metrics.json` | Session-window delivery bounds, starting-cohort results, coverage, pending/unknown counts, first remote latency, and observed duplicates rebuilt from the same event-log prefix; historical definitions stay legacy |
| `export.json` | Export time, run state, active/partial flags, and the captured source file sizes |

The export captures file sizes under the Controller's persistence lock and streams the ZIP after releasing that lock. Events appended later are excluded, so a slow download does not hold up telemetry writes. A completed run can still receive delayed telemetry: download again after collection has settled if you need those later records. `partial: false` indicates a terminal recorded run state, not a guarantee that no telemetry was lost. The archive does not contain message payloads, PCAP files, or the Prometheus/Grafana databases.

The result-list request only reads metadata, captures file identities and sizes, and returns a previously prepared `downloadBytes` value when that exact file/state version is still usable. It never builds a ZIP on a cache miss. The UI prepares a missing value with `HEAD /api/v1/experiments/RUN_ID/download`; HEAD runs the same streaming ZIP encoder into a byte counter and returns its exact `Content-Length` without a response body. A following GET uses the prepared measurement and streams the archive. The Controller does not retain the archive in memory or on disk. Concurrent misses for one run share one measurement, and at most two different runs are measured at once.

When no publication is pending, an unchanged file/state version keeps the measured byte count while each HEAD or download records a fresh, full-precision export time; the stored `export.json` entry uses a fixed-width timestamp so its encoded length stays constant. Stable list rows omit `downloadSizeMaxAgeMs`, and stable HEAD responses omit `X-KPL-Result-Size-Max-Age-Ms`. A result with pending publications briefly reuses both its byte count and export boundary. Its list row includes `downloadSizeMaxAgeMs`, and HEAD includes `X-KPL-Result-Size-Max-Age-Ms`; each value is a positive number of milliseconds calculated from the server's remaining cache lifetime after subtracting a one-second response safety grace. This relative lifetime avoids depending on synchronized client and server clocks. The first list that observes a prepared value extends the short lifetime once, then reports the max age from that extended boundary so an immediate GET still matches the displayed size; repeated list refreshes do not keep sliding the deadline. If the server has no lifetime left beyond the safety grace, the list omits both `downloadBytes` and `downloadSizeMaxAgeMs`, while HEAD measures a new boundary before responding.

The first HEAD after Controller startup or a source/state change must read the captured files and rebuild metrics; pending results repeat that work after their short cache expires. Canceling the HTTP request while it waits for a measurement slot or performs that work cancels the request without producing a server-error response. Large saved event logs can therefore take longer to measure. Running and queued rows skip background HEAD measurement, but downloading their snapshot still measures it and sends an exact `Content-Length`.

After a restart, a saved run that still says `running` or `queued` is displayed as `interrupted`; its original metadata is preserved in the ZIP. This is a display status, not evidence that its Peers have stopped. Saved results do not restore live experiment state, resume execution, or replay live counters. ZIP metrics are rebuilt from the retained log. An unreadable metadata file is displayed as `unreadable`; inspect the Controller logs and stored files before retrying.

**Saved results → Delete** permanently deletes the selected run's scenario, metadata, and events after confirmation. Running/queued experiments, members of an active batch, and results being downloaded are protected. The deletion API is `DELETE /api/v1/results/{id}` with the configured bearer token. Previously scraped Prometheus/Grafana history remains. Deletion markers prevent late telemetry from recreating a deleted result. Reconstructing ZIP metrics uses memory proportional to distinct event IDs and message/receiver pairs; the raw file copy itself is streamed.

The existing public GET policy also applies to the saved-result list and downloads. API clients can use:

```bash
curl --fail http://localhost:8080/api/v1/results
curl --fail --output run-results.zip \
  http://localhost:8080/api/v1/experiments/RUN_ID/download
```

Replace `RUN_ID` with an ID from the saved list. In Swarm, use the control node's address. With `KPL_STACK_NAME=kpl`, original files reside in the `kpl_controller-data` volume on that node. It is mounted at `/var/lib/kpl/data` in the Controller, with run files under `runs/<run-id>`. The manager's `swarm.sh remove` command preserves this volume. There is no automatic raw-event retention limit or cross-node replication; manage disk space and backups separately.

Exports contain collected telemetry, including subscription-session start/checkpoint/stop evidence and recorded Agent termination confirmations. Peer retries, Agent queue backpressure, and graceful drains reduce loss but have finite bounds; forced termination does not drain the Peer. Source sequence gaps make missing receipts unknown. A download cannot recover missing events or reveal completely invisible sessions. `/api/v1/events` still returns only the latest 300 events, and the web event view displays the newest 40 from that buffer, while the ZIP reads the full saved log.

## Metric definitions

| Metric | Meaning |
|---|---|
| `kpl_events_total` | Cumulative events received by the Controller, labeled by `run_id`, `agent_id`, `event_type`, and `topic` |
| `kpl_message_bytes_total` | Published/delivered PubSub data bytes, excluding libp2p framing and TCP/IP headers |
| `kpl_gossipsub_control_rpcs_total` | RPC envelopes containing each GossipSub control type, separated by `send`, `recv`, and local pre-send `drop` |
| `kpl_gossipsub_control_entries_total` | Repeated protobuf control entries carried in those RPCs |
| `kpl_gossipsub_control_message_ids_total` | Non-unique message-ID reference occurrences in IHAVE, IWANT, and IDONTWANT entries |
| `kpl_gossipsub_control_peer_exchange_records_total` | Peer-exchange records carried by PRUNE entries |
| `kpl_window_stable_pairs`, `kpl_window_reached_pairs` | Mature message/receiver-session pairs proven subscribed for the whole delivery window, and their on-time successes; labeled by `run_id` |
| `kpl_window_unknown_pairs`, `kpl_window_missed_pairs`, `kpl_window_late_pairs` | Unknown receipt, confirmed miss, and late-receipt counts among stable pairs |
| `kpl_window_delivery_ratio` (`bound`: `lower` / `upper`) | Logical bounds on stable conditional delivery, absent with no stable pairs; not confidence intervals |
| `kpl_window_initial_pairs`, `kpl_window_initial_reached_pairs`, `kpl_window_initial_unknown_pairs` | Known starting-cohort pairs, on-time successes including departed sessions, and unknown receipts |
| `kpl_window_initial_delivery_ratio` (`bound`: `lower` / `upper`) | Logical delivery bounds within the known starting cohort; absent only when that cohort is empty |
| `kpl_window_stable_coverage`, `kpl_window_stable_coverage_upper_bound` | Lower and upper coverage bounds: stable / known-starting, and (stable + continuity-unknown) / known-starting; absent only when the known starting cohort is empty |
| `kpl_window_departed_pairs`, `kpl_window_continuity_unknown_pairs` | Known starting pairs confirmed departed before the deadline, and pairs with neither proven departure nor proven continuity through the deadline |
| `kpl_window_publication_availability_unknown_pairs`, `kpl_window_availability_unknown_pairs` | Candidate pairs lacking proof of availability at publication, and the aggregate of publication-time and continuity uncertainty |
| `kpl_window_pending_publications`, `kpl_window_finalized_publications` | Messages before their deadline and messages whose window has elapsed; late telemetry can still revise finalized results |
| `kpl_window_measurement_incomplete`, `kpl_window_legacy_publications` | Known unscoped measurement streams (0/1), and publications using historical definitions; a zero flag does not detect completely invisible telemetry |
| `kpl_window_propagation_latency_seconds` | First on-time stable remote envelope delivery histogram, labeled by `run_id`, receiving `agent_id`, and `topic`; raw/local/late/departed/unknown/invalid samples excluded |
| `kpl_window_duplicate_copies`, `kpl_window_duplicates_per_reached_pair` | Observed extra PubSub copies within the window at successful stable remote pairs, and copies per successful pair |
| `kpl_operation_failures_total` | Publish/leave failures recorded under `onError: continue` |
| `kpl_telemetry_dropped_events_total` | Reported telemetry loss; neither P2P packet loss nor proof that unreported loss is zero |
| `kpl_nodes` | Peer counts by experiment, Agent, group, role, type, and state |
| `kpl_agent_*` | Agent online status, capacity, and latest heartbeat observed by the Controller |
| `kpl_experiment_*` | Experiment state, phase, and job state |
| `kpl_network_configured_*` | Effective configurations of starting/ready Peers, aggregated by group; delay includes mean/min/max, while jitter/loss report the mean |
| `kpl_local_*` | Each Agent's local Peer states, capacity, pending cleanup, and telemetry queue length |
| `go_*`, `process_*` | Runtime, CPU, and memory metrics for the scraped Controller/Agent processes; these do not represent total Peer container resource usage |

`kpl_network_configured_loss_ratio` is the configured packet loss ratio, not an observed loss rate. Configured delay is also distinct from measured RTT. The `graft`/`prune`/`remove_peer` events help analyze PubSub mesh changes; they are neither a TCP connection graph nor a complete mesh snapshot.

Control counters use only `run_id`, `agent_id`, `direction`, and `control_type` labels. The Topic filter does not apply because IWANT and IDONTWANT contain no topic and one RPC may contain several topics. `send` records outbound queue admission rather than remote receipt, `recv` precedes later router policy checks, and `drop` is a local queue/size rejection rather than `netem` loss. Read RPC, entry, message-ID-reference, and PRUNE peer-exchange counts as separate units. Exact per-RPC topic counts and the per-Agent totals in `metrics.json` remain in the result ZIP. See [experiment metric definitions](experiment-metrics.md#gossipsub-control-traffic).

For current relationships, the Dashboard's [interactive topology](topology.md) uses Peer status snapshots to display transport, Kademlia routing-table, and GossipSub mesh layers independently. These live snapshots do not depend on the recent-event buffer and are not an exhaustive historical graph in Prometheus or result exports.

`session-window-v1` uses actual publication time and `publish.deliveryWindow` (default 10s, positive and at most 1h). Session evidence must prove subscription throughout that window for the primary conditional ratio. Departures before the deadline are excluded even after early success; they remain in the starting-cohort ratio. Later joins and publisher-local receipts are excluded from both. Disconnection or mesh changes do not remove a subscriber. Show stable bounds, starting-cohort bounds, coverage, pending messages, and uncertainty together. A missing sequence means unknown, not a confirmed miss; an absent availability proof is not a confirmed departure.

Grafana session panels use only the Run filter and aggregate receiver-pair counts, not averages of percentages. Bounds describe observed cohorts; they do not establish an unseen population. Starting-cohort delivery and stable coverage remain available despite publication-time availability warnings or `measurementIncomplete`; they are N/A only when the selected runs contain no known starting pair. For each aggregation, known starting = stable + departed + continuity-unknown. The legacy-publications panel identifies historical data excluded from these results. New panels use `kpl_window_*` only; historical `kpl_delivery_*` and `kpl_propagation_latency_seconds` keep their old meanings and must not be combined.

Overall `deliver` counts include local delivery and cannot be divided by publication counts to obtain reachability. TCP retransmissions can turn packet loss into delay rather than message loss. See the [precise definitions, equations, and sources](experiment-metrics.md). Late batches can correct window gauges and histogram buckets; query them directly, not with `rate`/`increase`. Grafana latency shows cumulative whole-run quantiles, not quantiles restricted to the selected time range.

Cumulative counters are independent of the web interface's 300-event recent buffer. They reset when the Controller process restarts, and historical `events.jsonl` files are not replayed automatically. Time series already stored in Prometheus remain available, and `rate`/`increase` handle observed counter resets. They cannot recover events that disappeared before a scrape or telemetry that failed to arrive. Because `increase` estimates interval growth from scrape samples, it does not always exactly match integer cumulative event counts.

Raw deliveries contribute to delivery counts and bytes but not to the latency histogram. Do not interpret intervals without latency samples as 0 ms. At startup, each Peer has a five-second budget for up to seven Controller health samples, spacing fast failures by 250ms and using the minimum-RTT midpoint. Failure does not block Ready: an unsynchronized Peer retries every five seconds, while a synchronized Peer refreshes every 30 seconds. A sample expires after two minutes without success; the last offset remains for timestamp continuity, but trusted clock metadata stops until recovery. A bounded negative estimate can still prove on-time delivery, but it remains outside the latency histogram. Keep chrony/NTP enabled on all hosts. Checkpoint evidence describes instrumented application sessions rather than physical uptime.

## Execution validation

The historical runs below predate session-window measurement and included local latency samples. They validate the previous event-count implementation, not the new conditional delivery ratio or latency distribution. Compare new runs using the same `session-window-v1` definition and delivery window.

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

With Compose bind mounts, edits to dashboard JSON files in `monitoring/grafana/dashboards` are applied at 30-second intervals. Swarm configs are immutable: Prometheus and dashboard configuration changes use versioned config references and require a stack redeployment. The provisioned originals are managed as files. To save a separate dashboard, sign in as an administrator and work with a copy. See the [Grafana provisioning documentation](https://grafana.com/docs/grafana/latest/administration/provisioning/).

```bash
# Check scrape target status.
curl http://localhost:9090/api/v1/targets

# Validate the Prometheus configuration.
docker compose exec prometheus promtool check config /etc/prometheus/prometheus.yml

# Check service status and logs.
docker compose ps
docker compose logs --tail=100 prometheus grafana
```

The Compose file intentionally uses static Agent service names and does not discover remote hosts. The supplied Swarm stack instead discovers registered targets from `GET /api/v1/prometheus/agent-targets`. Use `sh scripts/swarm.sh access` to inspect the advertised URLs, ensure the configured port is free on every selected Agent node, and permit TCP traffic from the control node. If operators open an Agent metrics link directly, permit their browser's trusted management network as well; block untrusted sources. `up{job="kpl-agent"}` distinguishes successful scrapes from registered targets that are unreachable through a firewall or an incorrect Swarm `NodeAddr`.

Image versions are pinned to Prometheus `v3.13.2` and Grafana `13.2.1`. When upgrading, consult the official [Prometheus downloads](https://prometheus.io/download/) and [Grafana Docker installation guide](https://grafana.com/docs/grafana/latest/setup-grafana/installation/docker/), then revalidate the configuration and dashboards.
