# Publish While Workers Churn on Swarm

English | [Korean](swarm-churn-publish.kr.md)

Use [`examples/swarm-churn-publish.yaml`](../examples/swarm-churn-publish.yaml) to keep workers joining and leaving while repeatedly publishing from randomly selected live workers. It extends the small [Swarm smoke test](swarm.md#first-deployment-experiment-and-download) with ongoing churn and repeated observations. It is a starting configuration, not a measured capacity guarantee or an exact replay of a v2 experiment.

## Prepare and Run

First complete the [Swarm deployment workflow](swarm.md). Start with **at least two online Agents and 40 total available slots**, such as two otherwise idle Agents configured for capacity 20 each. In **Agent status**, count only `online` rows: **Peers** is occupied / capacity. The summary's **Available slots** can include offline Agents. Keep other experiments stopped for a useful comparison.

Use the same account and Docker context as deployment. The commands below use sudo; omit it consistently if your deployment account already has Docker access.

```sh
sudo sh scripts/swarm.sh status
sudo sh scripts/swarm.sh access
sudo sh scripts/swarm.sh credentials
sudo sh scripts/swarm.sh scenario examples/swarm-churn-publish.yaml
```

Open the Controller URL from `access`. Choose **Run experiment**, replace the entire **YAML scenario** field with the scenario output, enter the deployed API token from `credentials`, and click **Run**. `scenario` only prints YAML; the web action starts the experiment. The default web form and the helper's default `scenario` output are different examples.

Set **Runs** beside **Run** to repeat the entire scenario 1–100 times. Iterations run sequentially and get separate results; **Stop batch** or a failed iteration cancels the remainder. The explicit seed is reused. See [repetition and metric definitions](experiment-metrics.md).

Workers use Linux `NET_ADMIN` and the host kernel's netem support. Synchronize clocks between hosts before comparing one-way propagation latency. The example's network settings apply to outbound P2P TCP traffic; control and telemetry HTTP traffic are outside that configured scope.

## What Happens

| Stage | Behavior |
|---|---|
| Bootstrap | Create two stable `boot` Peers and wait for their readiness. They provide DHT bootstrap discovery without joining GossipSub or acting as stable publishers. |
| Churn | Start background job `worker-churn`: up to 10,000 sequential worker joins, balanced across available Agents, with exponential mean 4-second intervals. |
| Worker lifetime | Each worker has Pareto lifetime `xm: 60s`, `alpha: 2.5`, giving a theoretical mean of 100 seconds. The clock starts when Docker creation is acknowledged, including startup time. Expiration requests removal; actual cleanup can finish later. |
| Warm-up | Wait 3 minutes while churn continues. |
| Publishing | Run 30 sequential rounds. Each round randomly selects up to 10 eligible workers without replacement, then publishes sequentially with exponential mean 1-second gaps. |
| Round gaps | Wait 10 seconds between rounds, for 29 gaps in total. This explicit wait also prevents an empty candidate set from consuming all rounds immediately. |
| Collection and cleanup | After the final round, collect for 30 seconds while churn continues, then `stop-all` cancels the remaining join job and cleans up Peers. |

The theoretical steady-state worker count is `100s / 4s = 25`, plus two boot Peers. This is an expectation, **not a ceiling**. Lifetimes vary, Docker creation and startup take time, and a full Agent causes join admission to wait. These effects change the actual ready population and arrival rate. The 40-slot starting recommendation leaves some headroom but does not establish that a particular server can sustain this workload.

`count: 10000` is a **total join budget**, not 10,000 simultaneous workers. The final `stop-all` usually cancels the producer well before that budget is exhausted. This example has no explicit per-worker `leave` phase; sampled lifetimes drive departures.

Only the stable boot group has a readiness barrier. A worker `wait-ready` barrier over a continually growing churn group would also include stopped members from the current generation. The 3-minute warm-up avoids that unsuitable barrier, but does not guarantee a particular worker count or GossipSub mesh convergence. Worker mesh settings are `d: 9`, `dLow: 7`, and `dHigh: 11`.

A full 10-node round has nine sampled gaps, averaging about 9 seconds before request overhead. The nominal waits total `180 + 30×9 + 29×10 + 30 = 770 seconds`, or **about 12 minutes 50 seconds**, plus bootstrap, API, and cleanup time. It is not a deadline: empty or small rounds shorten it, and slow requests lengthen it.

## Who Publishes and What Is Measured

At each round's start, the Controller builds a fresh set of `ready`, publish-capable full workers on online Agents in the worker group, subscribed to `kpl/swarm-churn`. It shuffles this set and selects up to 10 distinct nodes. A node can be selected again in another round, and workers that joined later can participate in later rounds.

A selected worker can expire before its turn. `onError: continue` records individual failures as `phase-operation-failed` and continues; no eligible workers makes that round a no-op. Cancellation, scenario deadlines, persistence errors, and other fatal failures can still end the run. The maximum is **300 selected publish operations**, not 300 guaranteed successful messages.

Each publication uses `payloadSize: 32`, `payloadEncoding: envelope`, and topic `kpl/swarm-churn`. The 32 bytes describe the random payload. The envelope adds metadata, so the PubSub data and network traffic exceed 32 bytes. The envelope enables propagation latency measurement.

The worker network profile applies `scope: p2p`, 25ms delay, 2ms jitter, and 0.5% configured packet loss. Boot Peers have no added network impairment. **Packet loss is not the delivery/publication ratio**: TCP can retransmit, one publication can reach many subscribers, and the publisher's own local delivery is included.

## Observe and Save Results

In the Control Room, watch ready Peers, Agent occupancy, experiment/job state, and events. In Grafana, open **KP2PLab Experiment Analysis**, choose your `swarm-churn-random-publish` run in **Run**, and filter **Agent** and **Topic**. After completion, set the time range to the experiment window.

| Observe | Data |
|---|---|
| Changing population and occupied capacity | Live node inventory, `kpl_nodes`, `kpl_agent_active_nodes` |
| Publication, delivery, and duplicate event counts | `kpl_events_total`; use `graft`/`prune` events to observe mesh changes |
| Delivery ratio and average extra copies per successful remote delivery | `kpl_delivery_ratio`, `kpl_delivery_duplicates_per_reached_pair`; Grafana applies only the Run filter to these panels |
| Envelope latency and PubSub data volume | `kpl_propagation_latency_seconds`, `kpl_message_bytes_total` |
| Configured network conditions | `kpl_network_configured_*`: settings, not observed loss or RTT |
| Failed requests and reported telemetry drops | `kpl_operation_failures_total`, `kpl_telemetry_dropped_events_total` |

PubSub `join`/`leave` events describe topic membership, **not Peer creation and departure**. Node lifecycle is observed through inventory updates and sampled gauges; `events.jsonl` does not guarantee every lifecycle transition. New publications retain dispatch-time `targetNodeIds` and `cohortCapturedAt`: subsequent departures stay in the denominator and subsequent joins are excluded. ZIP `metrics.json` reconstructs delivery ratio, first remote latency, and average duplicates from the same saved log boundary. Legacy `targetNodes` counts alone cannot reconstruct recipient cohorts. See the [definitions and collection limits](experiment-metrics.md).

After the run ends, confirm Peer cleanup and, when no other runs are active, zero Agent occupancy. A `completed` run can have `canceledJobs: 1`: `stop-all` intentionally cancels the unfinished join job. That counter alone is not a failure.

Use **Download results** on the run or in **Saved results**. The ZIP includes the submitted scenario, run metadata, and full saved event log at the export boundary, independently of the 300-event recent buffer. It includes no per-node lifecycle/timestamp snapshot, message payload dump, PCAP, or Prometheus/Grafana database. Running downloads are snapshots, and missing telemetry cannot be recovered by downloading. [Archive contents and retention](monitoring.md#download-experiment-results)

Download before `sudo sh scripts/swarm.sh remove` takes the web services offline. The data volumes and external Peer network remain. A retained `interrupted` result after restart is not resumed and does not prove Peer cleanup.

## Adjust the Experiment

| Setting | Effect |
|---|---|
| Worker join `count` | Changes the total creation budget. Keep it high enough that joins continue through the last collection period. |
| Worker `interval.mean` | A smaller value raises attempted arrivals; Docker and capacity waits still add time. |
| Worker lifetime `xm` / `alpha` | Changes session lengths and expected population. For `alpha > 1`, mean lifetime is `alpha × xm / (alpha - 1)`. |
| Agent capacity | Changes admission limits, not reserved CPU/RAM. Measure host load before raising it. |
| Warm-up `duration` | Changes how long churn runs before the first publication. It is a timed wait, not a convergence test. |
| Publish `count` / interval | Changes the maximum publishers per round and spacing between their requests. |
| Round pairs / 10-second waits | Changes observation duration. Keep explicit waits so empty rounds still advance time. |
| Worker network profile | Changes P2P delay/jitter/loss. Keep the two bootstrap Peers stable for this experiment design. |
| `payloadEncoding` | `envelope` provides latency metadata. `raw` gives exactly 32 bytes of PubSub data here, but no propagation latency samples. |

The YAML uses anchors to reuse a publish phase and its wait phase. These are standard YAML references expanded during parsing, **not a scenario-engine loop feature**. To change the round count, change the repeated publish/wait pairs; editing the anchored publish phase changes every alias that refers to it. Preserve the final collection wait and `stop-all`.

A fixed seed makes random sampling reproducible when the eligible populations and execution conditions match. It does not make Docker timing, departures before a request, or results identical across different runs and machines.

## Relationship to the v2 Churn Experiments

The v2 churn scripts kept 10 boot nodes stable, scheduled up to 10,000 worker joins, waited, published one batch from up to 10 randomly selected workers using 32-byte raw data on all their topics, then collected for 30 seconds while churn continued.

This example deliberately uses two boot nodes, a smaller expected worker population, **30 newly selected publication rounds**, one explicit topic, added P2P network conditions, and **envelopes for latency measurement**. Its balanced Agent placement also differs from v2's per-join random worker-server selection. It preserves the churn-and-observe idea while making repeated observations practical on a small Swarm. It does not establish equivalent results or reconstruct missing v2 configuration files. See the [v2 reproduction notes](v2-reproduction.md) for the compatibility boundaries.
