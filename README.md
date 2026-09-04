# K-P2PLab v3

English | [한국어](README.kr.md)

K-P2PLab v3 is an experimentation platform for building standard libp2p Kademlia and PubSub networks across multiple hosts, running reproducible churn and publish scenarios, and observing state and propagation events in real time from the web. HopWave is intentionally outside the scope of this version.

## Architecture

```text
Browser / REST client
        │
        ▼
Controller ─── scenario state machine / registry / event store / dashboard
        │
        ├──────────────┐
        ▼              ▼
     Agent A         Agent B       capacity-aware scheduling / batched telemetry
      │  │            │  │
      ▼  ▼            ▼  ▼
    Peer Peer        Peer Peer     configurable libp2p Kademlia + PubSub
```

- **Controller** provides Agent and Peer state, the bootstrap registry, scenario execution, event persistence, the REST API, and the web dashboard.
- **Agent** runs once per physical or virtual host. It starts and stops Peer processes, forwards publish requests, and batches telemetry for the Controller.
- **Peer** derives its identity from a deterministic `(run ID, seed)` namespace and runs a selected Kademlia and PubSub configuration.
- **Dashboard** uses SSE to update Agent capacity, Peer readiness, connection topology, peer-score summaries, experiment phases, propagation latency, and recent events.

## Quick start

Go 1.24 or later, or Docker Compose, is required.

```bash
docker compose up --build
```

Open `http://localhost:8080` and select **실험 실행** (Run experiment) to run the default smoke scenario. To protect mutating API operations, set `KPL_API_TOKEN` before starting the stack and enter the same value in the dashboard's run dialog.

```bash
export KPL_API_TOKEN='replace-me'
docker compose up --build
```

To run local binaries, start the Controller and at least one Agent as separate processes.

```bash
go build -o bin/kpl ./cmd/kpl
./bin/kpl controller --listen :8080
./bin/kpl agent --id local-a --advertise-url http://127.0.0.1:8090 --controller-url http://127.0.0.1:8080
```

Validate a scenario without running it:

```bash
./bin/kpl validate --scenario examples/smoke.yaml
```

## Module path

The canonical Go module path is `github.com/k-p2p-lab/v3`. Source imports and commands should use that path.

## Node roles, types, and profiles

`role` controls bootstrap discovery: a `boot` node is advertised by the Controller as a bootstrap peer, while a `worker` node is not. `type` controls the node's libp2p, Kademlia, and PubSub behavior. They are deliberately independent, so an experiment can use several worker behaviors without abusing the bootstrap role. When both `type` and `profile` are omitted, `role: boot` selects the `boot` preset and every other role selects `full`.

| Built-in type | Behavior |
|---|---|
| `boot` | Kademlia-only bootstrap node by default, matching v2 behavior. |
| `full` / `worker` | Full Kademlia server and standard GossipSub participant with the v2 hard connection cap of `55`. |
| `light` | Kademlia client with a lower default connection limit. |
| `publisher` | Publish-only topic participant. |
| `subscriber` / `observer` | Subscribe-only participant; publishing is rejected. |
| `relay` | Relays subscribed topic traffic without publishing application messages. |
| `flood` | Uses FloodSub. |
| `random` | Uses RandomSub with separate minimum-degree and estimated-network-size controls. |
| `dht-only` | Runs Kademlia with PubSub disabled. |
| `gossip-only` | Runs PubSub with Kademlia disabled. |
| `non-gossip` / `mesh-only` | Uses the GossipSub mesh with lazy gossip disabled. |

Reusable node profiles belong in the top-level `profiles` map. A join phase resolves its configuration in this order: built-in `type`, named `profile`, then the phase's inline `node` overrides. Explicit `false` and `0` values are preserved, which allows experiments such as disabling lazy gossip with `historyGossip: 0`.

The built-in `boot` type sets `gossipsub.enabled: false`. Set it explicitly to `true` in a profile or inline `node` block when bootstrap nodes should also participate in PubSub. Once enabled, boot nodes can use every PubSub router, parameter, scoring, and inspection option described below; [`examples/mixed-workers.yaml`](examples/mixed-workers.yaml) demonstrates this opt-in.

```yaml
version: 2
name: mixed-workers
seed: 42
onExit: cancel
jobShutdownTimeout: 10s

profiles:
  tuned-mesh:
    type: full
    libp2p:
      connectionLimit: 128
      connectionManager:
        lowWater: 64
        highWater: 96
        gracePeriod: 30s
    kademlia:
      mode: server
      protocolPrefix: /k-p2p-lab/v3
      bucketSize: 20
      concurrency: 10
    gossipsub:
      router: gossipsub
      topics: [kpl/default]
      floodPublish: false
      peerExchange: true
      params:
        d: 8
        dLow: 6
        dHigh: 12
        dOut: 3
        dLazy: 8
        heartbeatInterval: 500ms

phases:
  - action: join
    group: tuned
    role: worker
    profile: tuned-mesh
    count: 100
    parallel: true
    parallelism: 16
```

### Protocol controls

The nested node configuration exposes the parameters supported by the pinned libp2p packages. Go duration strings such as `250ms`, `30s`, and `5m` are used for every duration field.

| Section | Configurable fields |
|---|---|
| `libp2p` | `userAgent`, `natPortMap`, `relay`, `relayService`, `connectionLimit`, `connectionManager.lowWater`, `connectionManager.highWater`, `connectionManager.gracePeriod`, `dialTimeout` |
| `kademlia` | `enabled`, `mode`, `protocolPrefix`, `protocolId`, `protocolExtension`, `bucketSize`, `concurrency`, `resiliency`, `lookupCheckConcurrency`, routing-table latency/refresh durations, `maxRecordAge`, provider/value/auto-refresh switches, optimistic-provide settings, and bootstrap timeout/retry interval |
| `gossipsub` | `enabled`, `router`, `topicMode`, `randomDegree`, `randomNetworkSize`, `subscribe`, `allowPublish`, `topics`, `floodPublish`, `peerExchange`, message/queue/validation limits, `signaturePolicy`, `seenMessagesTTL`, `subscriptionBufferSize`, and `scoreInspectInterval` |
| `gossipsub.params` | Every `GossipSubParams` field in the pinned library: mesh degrees, history, gossip factor/retransmission, heartbeat, fanout, prune/backoff, connectors, connection timeout, direct-connect and opportunistic-graft controls, IHAVE limits, IDONTWANT limits, and IWANT follow-up time |
| `gossipsub.score` | Global PeerScore weights and decay settings except application-defined P5, score thresholds, IP-colocation whitelist, and a complete `topics.<topic>` score block for mesh, first-delivery, failure, and invalid-message terms |

GossipSub-only controls (`params`, score/inspection, `floodPublish`, and `peerExchange`) apply only to the `gossipsub` router; FloodSub and RandomSub reject unsupported scoring options and use their router-specific controls instead.

The base mesh defaults are `D=6`, `DLow=5`, `DHigh=12`, `DScore=4`, `DOut=2`, `DLazy=6`, history `5/3`, gossip factor `0.25`, and a `1s` heartbeat. When PubSub is enabled on `boot`, that preset uses `DScore=3`. `full`/`worker` and the role-focused GossipSub worker presets inherit the v2-derived `DLow=5`, `DScore=3`, `maxIHaveLength=5500`, and `1s` initial heartbeat. Most worker presets also use the v2 hard connection limit of `55`; `light` uses `32`. Kademlia defaults to bucket size `20` and protocol prefix `/k-p2p-lab/v3`. See [`internal/model/config.go`](internal/model/config.go) for the complete typed schema and resolved defaults.

`libp2p.connectionLimit` is only a resource-manager hard cap on total connections; it does not implicitly create a soft connection manager. The soft low-water/high-water trimming behavior is installed only when `libp2p.connectionManager` is explicitly present, using its `lowWater`, `highWater`, and `gracePeriod`. This lets a profile choose the hard cap, the soft manager, or both independently.

When migrating a v2 configuration, map v2's misleadingly named `protocol_id` to v3 `kademlia.protocolPrefix`: v2 used that value as a prefix. In v3, `kademlia.protocolId` instead overrides the exact Kademlia V1 wire protocol ID; it is mutually exclusive with `protocolPrefix` and `protocolExtension`, so do not copy a v2 prefix into it. The default prefix `/k-p2p-lab/v3` is an intentional new protocol namespace. Use `protocolPrefix: /k-p2p-lab/kad-dht` only when an experiment requires the legacy v2 namespace.

For RandomSub, `randomDegree` sets the minimum connection/degree target through libp2p's process-global `RandomSubD`. KPL currently isolates each Peer in its own process, so that global applies to one Peer; an in-process multi-peer runtime would need additional isolation. `randomNetworkSize` is the separate estimated network-size argument passed to `NewRandomSub`.

When `gossipsub.score` is enabled, `scoreInspectInterval` defaults to `1s`. Each inspection updates the node's `peerScores` map. It is returned by `/api/v1/nodes`, `/api/v1/network`, `/api/v1/snapshot`, and the SSE snapshot stream; hovering a node in the topology displays the observed score count and average.

`appSpecificWeight` is intentionally unsupported. PeerScore's P5 term requires an in-process application-specific scoring callback, which cannot be supplied by the serializable scenario or REST configuration. Keep it at `0`; any non-zero value fails configuration validation explicitly instead of being accepted without effect.

## Scenarios and jobs

The recommended scenario format is version 2 YAML. It preserves the important v2 execution controls while adding explicit job tracking and readiness barriers. The old line-oriented `.kpl` DSL is not parsed directly; translate its commands into phases.

| Action | Purpose |
|---|---|
| `join` | Creates nodes using available Agent capacity. |
| `wait-ready` | Waits until a target percentage of a group/type is ready, optionally after named jobs or a `minCount` floor. |
| `publish` | Selects ready nodes from a group to publish messages. |
| `leave` | Selects and stops nodes from a group. |
| `wait` / `sleep` | Waits for a fixed duration. `sleep` is a v2-compatible alias. |
| `wait-jobs` | Waits for selected background job IDs, or all jobs when `jobs` is empty. |
| `log` | Writes a scenario message to the Controller log. |
| `stop-all` / `reset` | Cancels and drains every background job, then uses the run-generation fence to stop the experiment's current and older Peer generations. `reset` is an alias. |

For `join`, `count` is the exact number of create operations. For `publish` and `leave`, it is a maximum capped by the number of eligible candidate nodes; publish candidates must have PubSub and publishing enabled and must have joined the requested topic. `repeat` repeats the entire phase. `parallel` selects sequential or concurrent replica execution, while `parallelism` optionally caps concurrent operations. `await` defaults to `true`; setting it to `false` starts a tracked background job named by `job`, allowing later phases to run while churn or publishing continues. A `wait-jobs` phase can join selected jobs by name.

Background-job behavior at the natural end of the phase list is controlled by top-level `onExit`. Its default, `cancel`, cancels remaining jobs and then waits for them to stop. `onExit: drain` instead waits for them to complete naturally. A naturally successful completion applies this job policy but leaves Peer processes running unless the scenario contains an explicit `stop-all`.

`jobShutdownTimeout` defaults to `10s`. When a user or API request cancels a scenario, or when any scenario phase or background job fails, the Controller cancels outstanding jobs and waits for their termination within this bound, then asks every Agent to generation-fence and clean up Peer processes through the current generation. An explicit `stop-all` uses the same bounded job shutdown, resets job tracking, and fences the current run generation. The Agent records the monotonically increasing fence before stopping matching processes: a late create at generation N either committed before the fence and is included in cleanup, or is rejected because its generation is at or below the fence. After `stop-all` succeeds, the scenario advances to generation N+1, so later phases may create new nodes under the same run ID and may reuse job IDs.

`wait-ready` evaluates the complete matching cohort in the current run generation, including failed, stopping, and stopped nodes; nodes from previous generations are ignored. A failed cohort member prevents the barrier from succeeding, and a node reported as ready contributes to the ready count only while its Agent is online. Because the cohort still contains only nodes observed so far, `wait-ready` after an `await: false` join must specify either `jobs: [job-id]` or `minCount`. `jobs` waits for the selected producer jobs to finish before checking readiness; `minCount` leaves them running but prevents a partially created group from satisfying the ratio too early.

| `parallel` | `await` | Behavior |
|---|---|---|
| `false` | `true` | Paced replicas execute sequentially; the next phase waits. |
| `true` | `true` | Replicas execute concurrently; the next phase waits for the batch. |
| `false` | `false` | A paced sequential job runs in the background. |
| `true` | `false` | A concurrent job runs in the background. |

For v2 compatibility, a `publish` phase with no `interval` has a special default: with `parallel: true`, every operation gets a `1s` phase-start offset; sequential publish uses zero delay. This rule is independent of `await`.

`wait` durations and readiness/job timeouts must be positive. An omitted join `lifetime` means no automatic leave, while an explicitly sampled `0s` lifetime stops the new node immediately, matching v2.

```yaml
phases:
  - name: background churn
    action: join
    job: churn
    await: false
    parallel: false
    group: churners
    role: worker
    type: light
    count: 100
    interval: {model: exponential, mean: 250ms}
    lifetime: {model: pareto, xm: 45s, alpha: 2.5, max: 3m}

  - action: wait
    duration: 10s

  - action: wait-ready
    group: churners
    jobs: [churn]
    readyRatio: 1
    timeout: 5m
```

Intervals and lifetimes support these distributions. Duration-valued fields use Go duration strings, and optional `min`/`max` duration bounds clamp the result.

| Model | Parameters and sampling semantics |
|---|---|
| `fixed` | `value` is the exact duration. |
| `exponential` | `mean` is the mean duration of a continuous exponential sample. |
| `normal` | `mean` and `sigma` are duration-valued mean and standard deviation. Negative samples are clamped to zero before optional bounds are applied. |
| `pareto` | `xm` is the scale duration and `alpha` is the dimensionless shape. |
| `poisson` | v2-compatible behavior: `mean` is a duration, converted to seconds as the Poisson mean; the sampled result is quantized to an integer number of seconds. |
| `gamma` | `alpha` is the shape. Supply exactly one of v2-compatible `beta` (a rate per second) or `scale` (a duration); the two forms are mutually exclusive. |
| `lognormal` | `mu` is the log-space mean and sigma is dimensionless: use exactly one of a numeric string such as `sigma: "0.5"` or the numeric `logSigma` field. The sample is `exp(mu + sigma*Z)` seconds for standard normal `Z`. |

A non-zero scenario seed reproduces sampled delays, distribution samples, and random selection/order when the initial state, eligible candidate set, and readiness/job barriers are the same. Peer identity is deterministic per `(run ID, per-node seed)`: the same pair yields the same Peer ID, while different run IDs intentionally yield different Peer IDs and avoid cross-run identity collisions. Thus the same scenario seed reproduces sampling and ordering across runs, but not Peer IDs when their run IDs differ; `seed: 0` intentionally chooses a time-based seed. Start with [`examples/smoke.yaml`](examples/smoke.yaml), then see [`examples/mixed-workers.yaml`](examples/mixed-workers.yaml) for custom profiles, heterogeneous worker types, bounded parallel batches, and background paced jobs.

## REST API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/health` | Controller health |
| `GET` | `/api/v1/snapshot` | Full dashboard snapshot, including node `peerScores` |
| `GET` | `/api/v1/agents` | Agent state |
| `GET` | `/api/v1/nodes` | Peer state, including inspected `peerScores` |
| `GET` | `/api/v1/network` | Peers with `peerScores`, connection edges, and propagation metrics |
| `GET` | `/api/v1/bootstrap?runId={runId}` | Ready bootstrap peers belonging only to the required run ID |
| `GET` | `/api/v1/events` | Recent trace events |
| `GET` | `/api/v1/stream` | Real-time snapshot SSE stream, including `peerScores` |
| `GET` | `/api/v1/experiments` | Experiment state plus `activeJobs`, `completedJobs`, `failedJobs`, and `canceledJobs` counters |
| `POST` | `/api/v1/experiments` | Start a YAML scenario |
| `POST` | `/api/v1/experiments/{id}/stop` | Cancel a running experiment, then perform bounded job shutdown and generation-fenced Peer cleanup |

The `runId` query parameter on `/api/v1/bootstrap` is required. The registry returns only ready `boot` nodes with usable identity and address data from that run, so concurrent experiments cannot discover one another's bootstrap peers.

Example:

```bash
curl -X POST http://localhost:8080/api/v1/experiments \
  -H 'Content-Type: application/yaml' \
  --data-binary @examples/smoke.yaml
```

Raw events are stored at `data/runs/<run-id>/events.jsonl`; the exact input is stored as `scenario.yaml` in the same directory.

The Agent exposes this internal operational endpoint for Controller-driven cleanup:

| Method | Agent path | Description |
|---|---|---|
| `DELETE` | `/api/v1/runs/{runId}/nodes?generation=N` | Requires an unsigned `generation`; atomically raises the run fence through N, rejects later creates at generation N or below, stops existing nodes in those generations, and returns `202 Accepted` |

This endpoint and the other Controller-to-Agent registration, heartbeat, node lifecycle, publish, and batched-telemetry endpoints use REST/JSON. They are internal cluster and operations APIs, not client-facing Controller APIs, and may change independently. When `KPL_API_TOKEN` is set, mutating API calls require `Authorization: Bearer <token>`; GET and HEAD remain read-only.

The same four job counters are present in `/api/v1/snapshot` and SSE snapshots. The dashboard displays them on each run, so active, successful, failed, and canceled background work is visible without inspecting Controller logs.

## Measurement notes and limitations

Propagation messages include the publisher's timestamp. Synchronize all Agent hosts with chrony or NTP before comparing latency across physical machines. Events with a negative observed latency are marked with `clockSkewDetected`.

The default Agent runtime is a scale mode that manages multiple Peer processes directly. Experiments requiring per-node Linux `netem` isolation need an additional network-namespace or container runtime backend for the Agent. HopWave is not supported.

Stopped-node history is currently retained in memory and included in Agent heartbeats and Controller snapshots. Long-running, high-volume churn still needs a bounded retention policy and a separate paginated history API to prevent control-plane state and payloads from growing indefinitely.
