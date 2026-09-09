# REST API

English | [Korean](api.kr.md)

The Controller exposes the following public and operational endpoints. When `KPL_API_TOKEN` is configured, clients must send it as a Bearer token for mutation requests.

## Controller endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/metrics` | Prometheus exposition for Controller and experiment metrics |
| `GET` | `/api/v1/health` | Controller health and current UTC time used for Peer clock sampling |
| `GET` | `/api/v1/ui-config` | Published Prometheus and Grafana ports used by Dashboard navigation |
| `GET` | `/api/v1/prometheus/controller-targets` | Public Controller metrics endpoint as a Prometheus HTTP service-discovery group |
| `GET` | `/api/v1/prometheus/agent-targets` | Prometheus HTTP service-discovery groups for online Agents with advertised metrics URLs |
| `GET` | `/api/v1/snapshot` | Full dashboard snapshot, including node `peerScores` |
| `GET` | `/api/v1/agents` | Agent state |
| `GET` | `/api/v1/nodes` | Peer state, including inspected `peerScores` |
| `GET` | `/api/v1/network` | Peers with `peerScores`, connection edges, and propagation metrics |
| `GET` | `/api/v1/bootstrap?runId={runId}` | Ready bootstrap peers belonging only to the required run ID |
| `GET` | `/api/v1/discovery?runId={runId}&topic={topic}&requesterNodeId={nodeId}` | Ready, online PubSub transport candidates from the same run and exact topic |
| `GET` | `/api/v1/events` | Recent trace events |
| `GET` | `/api/v1/stream` | Real-time snapshot SSE stream, including `peerScores` |
| `GET` | `/api/v1/experiments` | Experiment state plus `activeJobs`, `completedJobs`, `failedJobs`, and `canceledJobs` counters |
| `GET` / `POST` | `/api/v1/scenarios` | List scenario summaries or save validated `{name, yaml}` |
| `POST` | `/api/v1/scenarios/validate` | Validate raw YAML without saving or running; public, no token required |
| `GET` / `PUT` / `DELETE` | `/api/v1/scenarios/{id}` | Load, update, or delete one saved scenario |
| `GET` | `/api/v1/results` | Saved experiment results, including runs from previous Controller sessions |
| `DELETE` | `/api/v1/results/{id}` | Delete an inactive saved result; active batches and downloads are protected |
| `GET` | `/api/v1/experiments/{id}/analysis` | Chart distributions, timelines and metrics from saved events and observations |
| `GET` / `POST` | `/api/v1/analysis-jobs/{id}` | Inspect / submit background analysis. Duplicate requests reuse work; `?refresh=1` requests a new snapshot |
| `GET` / `HEAD` | `/api/v1/analysis-jobs/{id}/result?jobId={jobId}` | Download persisted analysis JSON. Unfinished or mismatched attempts return `409` |
| `GET` / `HEAD` | `/api/v1/analysis-jobs/{id}/summary?jobId={jobId}` | Compact comparison artifact with aggregates, distributions and fits; omits per-message paths and source timelines |
| `GET` / `HEAD` | `/api/v1/experiments/{id}/download` | Download the scenario, metadata, events, optional observations and derived metrics as ZIP, or measure its size without a response body |
| `POST` | `/api/v1/experiments` | Run YAML once, or JSON `{scenario, repetitions}` for 1–100 sequential runs |
| `POST` | `/api/v1/experiments/{id}/stop` | Cancel a running experiment, then perform bounded job shutdown and generation-fenced Peer cleanup |

The `runId` query parameter on `/api/v1/bootstrap` is required. The registry returns only ready `boot` nodes with usable identity and address data from that run, so concurrent experiments cannot discover one another's bootstrap peers. `/api/v1/discovery` requires all three shown query parameters, excludes the requester, and returns configured topic participants rather than observed delivery or mesh outcomes. `/api/v1/prometheus/agent-targets` is a read-only operational endpoint used by the supplied Swarm Prometheus configuration; it omits offline Agents and Agents without a valid metrics URL.

The bootstrap response is an array of `{nodeId, peerId, addresses}` records (or `null` when empty). Unlike discovery, bootstrap does not filter on Agent online status. Discovery returns an array, empty when no candidates match, with an additional `subscribed` flag. It returns the full eligible candidate set; the requesting Peer applies rendezvous ranking, connection budgets, and retries as described in [topology](topology.md#bootstrap-and-topic-discovery).

`/api/v1/prometheus/controller-targets` advertises the Controller `--metrics-url` (`KPL_CONTROLLER_METRICS_URL`) and returns `[]` when it is unset. It is a public, read-only endpoint; the URL must be HTTP(S), end in `/metrics`, and have no credentials, query, fragment, or loopback/unspecified address. The Swarm helper supplies the control node address and `KPL_HTTP_PORT` automatically.

## Submit, stop, and observe runs

`POST /api/v1/scenarios/validate` accepts a raw YAML body (`Content-Type: application/yaml`), limited to 1 MiB. A valid scenario returns `200` with `{valid: true, name, phases}`. Empty input, YAML syntax errors, unknown fields, invalid settings, and multiple YAML documents return `400` with `{error: "…"}`; parser diagnostics retain line numbers when available. Bodies over the limit return `413`. This endpoint uses the same parser as save/run, creates no records or jobs, and does not require a token. Validation checks configuration, not Agent capacity or runtime connectivity.

`POST /api/v1/experiments` returns `202` with the first run's experiment object. Use `Content-Type: application/json` for `{scenario, repetitions}`; `scenario` is a YAML string and omitted `repetitions` defaults to `1`. Other content types are treated as raw YAML. Raw YAML and the decoded JSON `scenario` string are limited to 1 MiB. The JSON request envelope allows `6 * 1 MiB + 64 KiB` for escaping. An oversized request envelope returns `413`; a decoded YAML string over its limit or invalid scenario/repetition returns `400`.

Repeated submissions reserve a separate run ID and result record for every iteration, sharing `batchId`, `iteration`, and `repetitions`. Runs execute sequentially; a failed or canceled iteration cancels the queued remainder. Stopping any member while its repetition batch is still active cancels that batch. For `repetitions > 1`, every iteration, including the last, fences and removes its Peers before finalization. Only a naturally successful single run may retain Peers when its YAML omits `stop-all`.

The stop endpoint returns `202` once cancellation is requested, before cleanup completes. Observe `/api/v1/experiments` or the snapshot until the final state is recorded. A run with no remaining cancellation handle returns `404`. SSE sends an initial `event: snapshot`, subsequent full snapshots on state updates, and full snapshots every 15 seconds even without events; it does not provide event-ID replay. `/api/v1/events` contains at most the 300 most recent events across live Controller state.

From the repository root, replace `control-node:8080` with the Controller address printed by `sh scripts/swarm.sh access` and export the token printed by `sh scripts/swarm.sh credentials` as `KPL_API_TOKEN`:

```bash
curl -X POST http://control-node:8080/api/v1/experiments \
  -H 'Content-Type: application/yaml' \
  -H "Authorization: Bearer ${KPL_API_TOKEN:?Set KPL_API_TOKEN}" \
  --data-binary @examples/smoke.yaml
```

The scenario library endpoints store reusable editor inputs independently of experiment results. The list omits YAML, while individual GET, POST, and PUT responses include it. See the [scenario library guide](scenario-library.md) for the UI workflow, payloads, validation limits, and storage location.

Raw events, including typed bandwidth samples, are stored at `<data-dir>/runs/<run-id>/events.jsonl`; the exact input is stored as `scenario.yaml`, experiment metadata as `experiment.json`, and collected topology/score summaries as optional `observations.jsonl` in the same directory. The local Controller data directory defaults to `data`. In Swarm, the persistent `controller-data` volume is mounted at `/var/lib/kpl/data`, and each run's files are under `/var/lib/kpl/data/runs/<run-id>`.

Use **Download results** in the Dashboard to export a run as ZIP. **Saved results** also lists files retained from previous Controller sessions; **Refresh** reloads that list. Running experiments offer **Download snapshot**, which contains the records saved when the download starts. These exports include the full saved event log, independently of the 300-event recent buffer. See [result downloads](monitoring.md#download-experiment-results) for archive contents and collection limits.

`DELETE /api/v1/results/{id}` returns `204` on deletion, `404` if the result is absent, and `409` while the run/batch is active or an actual `GET` download holds it. Automatic `HEAD` size calculations and list reads do not cause a download conflict.

## Internal cluster endpoints

These REST/JSON endpoints serve component communication. Registration, heartbeats, and forwarded events are sent **to the Controller**; lifecycle commands are sent **to an Agent**.

| Caller → server | Method | Path | Purpose |
|---|---|---|---|
| Agent → Controller | `POST` | `/api/v1/agents/register` | Register the Agent instance |
| Agent → Controller | `POST` | `/api/v1/agents/heartbeat` | Report Agent and Peer snapshots |
| Agent → Controller | `POST` | `/api/v1/events/batch` | Forward up to 5000 events per batch |
| Controller → Agent | `GET` | `/api/v1/status` | Refresh Agent and Peer state |
| Controller → Agent | `POST` | `/api/v1/nodes` | Create a Peer from `CreateNodeRequest` |
| Controller → Agent | `DELETE` | `/api/v1/nodes/{nodeId}` | Request one Peer's shutdown |
| Controller → Agent | `POST` | `/api/v1/nodes/{nodeId}/publish` | Proxy a publish request |
| Peer → Agent | `POST` | `/api/v1/nodes/{nodeId}/status` | Report this Peer's latest status |
| Peer → Agent | `POST` | `/api/v1/telemetry` | Submit a telemetry batch |
| Agent → Peer | `GET` / `POST` | `/health` / `/publish` | Check readiness or publish through the Peer HTTP API |

Telemetry requests are limited to 5000 events and a 10 MiB JSON body. Peer and Agent senders split batches by their encoded byte size, including escaping and the envelope; retries retain source order and event identities. A successful prefix is removed before sending the remaining events. Both receivers validate every event before admission: a single event that cannot fit in a 10 MiB encoded batch returns `413`, with none of that request admitted. The Agent checks after normalizing its identity. This also prevents direct Controller submissions from creating oversized event lines that archive analysis cannot read.

JSON bodies must contain one value. Trailing whitespace is allowed within the body limit; a second JSON value, trailing garbage, or a body over the decoder limit returns `400`. Generic Controller/Agent JSON handlers use a 10 MiB limit; Peer `/publish` uses 1 MiB. Scenario request envelopes have the separate limits described above. A locally generated Peer event that cannot be JSON-encoded or fit in a batch is logged and counted in `telemetry_drop`, preserving its source sequence gap while later events continue. Network failures retain the pending batch for retry.

The Agent additionally exposes this Controller-driven cleanup endpoint:

| Method | Agent path | Description |
|---|---|---|
| `DELETE` | `/api/v1/runs/{runId}/nodes?generation=N` | Requires an unsigned `generation`; atomically raises the run fence through N, rejects later creates at generation N or below, stops existing nodes in those generations, and returns `202 Accepted` |

Internal endpoints may change independently of the operator API. Request and snapshot field definitions are in [`internal/model/model.go`](../internal/model/model.go), with bandwidth types in [`internal/model/bandwidth.go`](../internal/model/bandwidth.go); handlers are in [`internal/controller/api.go`](../internal/controller/api.go) and [`internal/agent/api.go`](../internal/agent/api.go).


## Detailed Peer logs

Updated Peers add metadata actually exposed by upstream libp2p to `events.jsonl`. Existing publish/deliver/duplicate events and main Metrics aggregation remain in use. `messageId` is an application identity; `fields.pubsubMessageId` and detailed ID lists encode pubsub wire IDs as hex.

| Event / field | Meaning |
|---|---|
| `rpcMetadataVersion`, `rpcObservationId` on `send_*`, `recv_*`, `drop_*` | Format version `1` and identity of one local RPC callback. Each event retains its own `eventId` |
| `rpcMessageCount`, `rpcSubscriptionCount` | Complete data-message and subscription counts for that RPC |
| IHAVE `topicMessageIds` | Advertised IDs grouped by topic |
| IWANT/IDONTWANT `messageIds` | Requested/unwanted IDs; these protocol entries carry no topic |
| `messageIdsComplete`, `omittedMessageIds` | Completeness and omitted count; `messageIdCount` keeps the full total |
| PRUNE `peerExchangeIdsByTopic`, `peerExchangeIdsComplete`, `omittedPeerExchangeIds` | Actual trace peer-exchange IDs by topic and completeness |
| `rpc_metadata`: `direction`, `messages`, `subscriptions` | send/recv/drop, `{topic,pubsubMessageId}` records, `{topic,subscribe}` records. A separate event is emitted only when data/subscription metadata exists |
| `subscriptionsComplete`, `omittedSubscriptions` | Subscription-list completeness and omissions |
| `pubsub_reject`: `reason`, `pubsubMessageId` | Rejection reason and wire ID reported by libp2p |
| `timestampSource`, `sourceTimestamp` | Trace time provenance and original unadjusted time. Missing upstream time uses `peer-clock` without fabricating a source timestamp |
| `clockBasis`, `clockOffsetMs`, `clockUncertaintyMs` | Added when a valid Controller clock synchronization estimate exists |

Detailed IDs are limited to 8,192 entries and 512KiB of hex per category; subscription lists are capped at 8,192. RPC/entry/ID totals remain complete. Existing `telemetry_drop` and source sequences expose event loss. Payload bodies, IWANT sender-queue causes and nonexistent global RPC IDs are not collected. Eager/Lazy follows the metadata estimation rules in [visualization](visualization.md).

The current completed `analysisVersion` is `3`. Full artifacts include `research` definitions, per-message paths/populations, `linkEstimate` and `evidence`. `/summary` preserves `messageCount`, aggregates, distributions and fits. Requests regenerate outdated caches; regeneration cannot recreate missing source metadata.

## Authentication

`KPL_API_TOKEN` is one shared Bearer credential for mutating KPL APIs, not a Swarm join token, Docker permission, or Grafana password. Use the same value for the Controller and every Agent; Agents pass it to their Peers automatically. It is required by the Swarm stack. There are no per-user roles or scoped tokens.

Enter the value in the dashboard's **Run experiment → API token** field. Running, saving, updating, or deleting through that dialog saves it in that origin's browser `localStorage` for later mutation requests; it does not expire automatically. REST clients send `Authorization: Bearer <token>`. GET reads, including state, events, SSE, and metrics, stay public. The stateless `POST /api/v1/scenarios/validate` is also public; this exception applies only to that exact method and path. The Controller also exempts HEAD; Agents and Peers only exempt GET. The token does not encrypt HTTP traffic.

The same four job counters are present in `/api/v1/snapshot` and SSE snapshots. The dashboard displays them on each run, so active, successful, failed, and canceled background work is visible without inspecting Controller logs.

See [visualization](visualization.md) for analysis responses, charts and export formats.

[Bandwidth measurement](bandwidth.md) · [v2 analysis coverage audit](v2-analysis-coverage.md)
