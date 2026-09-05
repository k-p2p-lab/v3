# Scenario Configuration Reference

English | [Korean](scenario-reference.kr.md)

This guide describes the version 2 YAML format, Peer profiles, protocol controls, network conditions, and background jobs. For saving reusable YAML in the Dashboard, see the [scenario library](scenario-library.md).

## Node roles, types, and profiles

`role` controls Kademlia bootstrap discovery: a `boot` node is advertised by the Controller as a bootstrap peer, while a `worker` node is not. Topic transport discovery is separate and considers ready PubSub participants in the same run and exact topic. `type` controls the node's libp2p, Kademlia, and PubSub behavior. These concepts are independent, so an experiment can use several worker behaviors without abusing the bootstrap role. When both `type` and `profile` are omitted, `role: boot` selects the `boot` preset and every other role selects `full`.

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

The built-in `boot` type sets `gossipsub.enabled: false`. Set it explicitly to `true` in a profile or inline `node` block when bootstrap nodes should also participate in PubSub. Once enabled, boot nodes can use every PubSub router, parameter, scoring, and inspection option described below; [`examples/mixed-workers.yaml`](../examples/mixed-workers.yaml) demonstrates this opt-in. PubSub-enabled Peers query the Controller's same-run, exact-topic registry immediately after startup and every three seconds, then use rendezvous hashing to select up to `DHigh` transport candidates per topic and open missing connections. GossipSub still forms the actual GRAFT mesh; DHT bootstrap, transport candidacy, and mesh membership remain distinct.

```yaml
version: 2
name: mixed-workers
seed: 42
onExit: cancel
jobShutdownTimeout: 3m

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

The base mesh defaults are `D=6`, `DLow=5`, `DHigh=12`, `DScore=4`, `DOut=2`, `DLazy=6`, history `5/3`, gossip factor `0.25`, and a `1s` heartbeat. When PubSub is enabled on `boot`, that preset uses `DScore=3`. `full`/`worker` and the role-focused GossipSub worker presets inherit the v2-derived `DLow=5`, `DScore=3`, `maxIHaveLength=5500`, and `1s` initial heartbeat. Most worker presets also use the v2 hard connection limit of `55`; `light` uses `32`. Kademlia defaults to bucket size `20` and protocol prefix `/k-p2p-lab/v3`. See [`internal/model/config.go`](../internal/model/config.go) for the complete typed schema and resolved defaults.

`libp2p.connectionLimit` is only a resource-manager hard cap on total connections; it does not implicitly create a soft connection manager. The soft low-water/high-water trimming behavior is installed only when `libp2p.connectionManager` is explicitly present, using its `lowWater`, `highWater`, and `gracePeriod`. This lets a profile choose the hard cap, the soft manager, or both independently.

When migrating a v2 configuration, map v2's misleadingly named `protocol_id` to v3 `kademlia.protocolPrefix`: v2 used that value as a prefix. In v3, `kademlia.protocolId` instead overrides the exact Kademlia V1 wire protocol ID; it is mutually exclusive with `protocolPrefix` and `protocolExtension`, so do not copy a v2 prefix into it. The default prefix `/k-p2p-lab/v3` is an intentional new protocol namespace. Use `protocolPrefix: /k-p2p-lab/kad-dht` only when an experiment requires the legacy v2 namespace.

For RandomSub, `randomDegree` sets the minimum connection/degree target through libp2p's process-global `RandomSubD`. KPL currently isolates each Peer in its own process, so that global applies to one Peer; an in-process multi-peer runtime would need additional isolation. `randomNetworkSize` is the separate estimated network-size argument passed to `NewRandomSub`.

When `gossipsub.score` is enabled, `scoreInspectInterval` defaults to `1s`. Each inspection updates the node's `peerScores` map. It is returned by `/api/v1/nodes`, `/api/v1/network`, `/api/v1/snapshot`, and the SSE snapshot stream; hovering a node in the topology displays the observed score count and average.

`appSpecificWeight` is intentionally unsupported. PeerScore's P5 term requires an in-process application-specific scoring callback, which cannot be supplied by the serializable scenario or REST configuration. Keep it at `0`; any non-zero value fails configuration validation explicitly instead of being accepted without effect.

### Per-node network conditions

With the Docker runtime, add `network` to a profile or a join phase's `node` block. The default `scope: p2p` shapes outgoing P2P TCP on port `20000` inside each Peer container. Select `scope: all` to reproduce v2's shaping of all egress, including control HTTP and telemetry. A configured delay is a one-way egress delay, not a round-trip latency target.

```yaml
node:
  network:
    delay: 100ms
    jitter: 10ms
    lossPercent: 1
    duplicatePercent: 0.1
    corruptPercent: 0.1
    reorderPercent: 1
    rateMbps: 10
    queueLimit: 1000
```

| Field | Meaning |
|---|---|
| `delay`, `jitter` | Go durations for added delay and its variation. Jitter requires a positive delay. |
| `lossPercent`, `duplicatePercent`, `corruptPercent`, `reorderPercent` | Packet percentages from `0` to `100`; reordering requires a positive delay. |
| `rateMbps` | Non-negative egress rate in megabits per second; `0` disables the rate limit. |
| `queueLimit` | Positive integer specifying the maximum packets in the netem queue. |
| `scope` | `p2p` (default) or `all` outgoing traffic. |
| `delayDistribution` | Samples one base delay per Peer using the interval distribution schema; mutually exclusive with `delay`. |
| `jitterDistribution` | `normal` (default), `uniform` (v2 behavior), `pareto`, or `paretonormal`. |
| `reorderCorrelationPercent` | Reordering correlation, corresponding to v2's `reorder.chance`. |
| `tbf` | Token bucket with `rateMbps`, `burstKbit`, and `latency`; mutually exclusive with positive netem `rateMbps`. |

See the [v2 reproduction audit and mapping](v2-reproduction.md) and [churn example](../examples/v2-churn.yaml). They cover placement (`balanced`, per-node `random`, batch `single-agent`, or explicit `agentId`), `onError: continue` for churn, `payloadEncoding: raw` for exact PubSub data length, and `topic: '*'` for all topics. Network scope and payload encoding retain their existing defaults. Worker bootstrap now uses seeded first-success selection, and the transport stack is explicitly TCP/Noise/Yamux.

Peers with network conditions require Linux `NET_ADMIN` and host-kernel `sch_netem` support. The Docker runtime adds `NET_ADMIN` only to those Peers. Missing kernel support or a failed `tc` command fails node startup explicitly. A process-runtime Agent rejects network conditions rather than applying rules to its shared host interface.

[`examples/network-conditions.yaml`](../examples/network-conditions.yaml) creates two bootstrap nodes and four constrained workers, waits for initialization, publishes sample messages, and stops all nodes. Run it from the dashboard or submit it with:

```bash
curl -X POST http://localhost:8080/api/v1/experiments \
  -H 'Content-Type: application/yaml' \
  -H "Authorization: Bearer ${KPL_API_TOKEN:-}" \
  --data-binary @examples/network-conditions.yaml
```

## Scenarios and jobs

The recommended scenario format is version 2 YAML. It preserves the important v2 execution controls while adding explicit job tracking and readiness barriers. The old line-oriented `.kpl` DSL is not parsed directly; translate its commands into phases.

| Action | Purpose |
|---|---|
| `join` | Creates nodes using available Agent capacity. |
| `wait-ready` | Waits until a target percentage of a group/type is ready, optionally after named jobs or a `minCount` floor. |
| `publish` | Selects ready nodes from a group to publish messages. `deliveryWindow` sets the per-message receipt deadline (default `10s`, positive, at most `1h`). |
| `leave` | Selects and stops nodes from a group. |
| `wait` / `sleep` | Waits for a fixed duration. `sleep` is a v2-compatible alias. |
| `wait-jobs` | Waits for selected background job IDs, or all jobs when `jobs` is empty. |
| `log` | Writes a scenario message to the Controller log. |
| `stop-all` / `reset` | Cancels and drains every background job, then uses the run-generation fence to stop the experiment's current and older Peer generations. `reset` is an alias. |

For `join`, `count` is the exact number of create operations. For `publish` and `leave`, it is a maximum capped by the number of eligible candidate nodes; publish candidates must have PubSub and publishing enabled and must have joined the requested topic. `repeat` repeats the entire phase. `parallel` selects sequential or concurrent replica execution, while `parallelism` optionally caps concurrent operations. `await` defaults to `true`; setting it to `false` starts a tracked background job named by `job`, allowing later phases to run while churn or publishing continues. A `wait-jobs` phase can join selected jobs by name.

Background-job behavior at the natural end of the phase list is controlled by top-level `onExit`. Its default, `cancel`, cancels remaining jobs and then waits for them to stop. `onExit: drain` instead waits for them to complete naturally. A naturally successful completion applies this job policy but leaves Peer processes running unless the scenario contains an explicit `stop-all`.

`jobShutdownTimeout` defaults to `3m`. When a user or API request cancels a scenario, or when any scenario phase or background job fails, the Controller cancels outstanding jobs and waits for their termination within this bound, then asks every Agent to generation-fence and clean up Peer processes through the current generation. An explicit `stop-all` uses the same bounded job shutdown, resets job tracking, and fences the current run generation. The Agent records the monotonically increasing fence before stopping matching processes: a late create at generation N either committed before the fence and is included in cleanup, or is rejected because its generation is at or below the fence. After `stop-all` succeeds, the scenario advances to generation N+1, so later phases may create new nodes under the same run ID and may reuse job IDs.

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

A non-zero scenario seed reproduces sampled delays, distribution samples, and random selection/order when the initial state, eligible candidate set, and readiness/job barriers are the same. Peer identity is deterministic per `(run ID, per-node seed)`: the same pair yields the same Peer ID, while different run IDs intentionally yield different Peer IDs and avoid cross-run identity collisions. Thus the same scenario seed reproduces sampling and ordering across runs, but not Peer IDs when their run IDs differ; `seed: 0` intentionally chooses a time-based seed. Start with [`examples/smoke.yaml`](../examples/smoke.yaml), then see [`examples/mixed-workers.yaml`](../examples/mixed-workers.yaml) for custom profiles, heterogeneous worker types, bounded parallel batches, and background paced jobs.

## Execution and retention limits

The Docker runtime isolates each Peer and supports per-node P2P egress conditions. `wait-ready` confirms Peer initialization and API readiness; it does not verify mesh convergence. Periodic topic discovery repairs transport candidates under churn, while GossipSub heartbeat and GRAFT processing still need time to converge. Add a settling phase when the experiment needs it. Scenario seeds reproduce application sampling and ordering under the documented conditions, but do not promise identical kernel packet impairment or network timing. HopWave is not supported.

Stopped-node history is currently retained in memory and included in Agent heartbeats and Controller snapshots. Long-running, high-volume churn still needs a bounded retention policy and a separate paginated history API to prevent control-plane state and payloads from growing indefinitely.
