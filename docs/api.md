# REST API

English | [Korean](api.kr.md)

The Controller exposes the following public and operational endpoints. When `KPL_API_TOKEN` is configured, clients must send it as a Bearer token for mutation requests.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/health` | Controller health and current UTC time used for Peer clock sampling |
| `GET` | `/api/v1/ui-config` | Published Prometheus and Grafana ports used by Dashboard navigation |
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
| `GET` / `PUT` / `DELETE` | `/api/v1/scenarios/{id}` | Load, update, or delete one saved scenario |
| `GET` | `/api/v1/results` | Saved experiment results, including runs from previous Controller sessions |
| `DELETE` | `/api/v1/results/{id}` | Delete an inactive saved result; active batches and downloads are protected |
| `GET` | `/api/v1/experiments/{id}/download` | Download a ZIP of the saved scenario, metadata, and collected events |
| `POST` | `/api/v1/experiments` | Run YAML once, or JSON `{scenario, repetitions}` for 1–100 sequential runs |
| `POST` | `/api/v1/experiments/{id}/stop` | Cancel a running experiment, then perform bounded job shutdown and generation-fenced Peer cleanup |

The `runId` query parameter on `/api/v1/bootstrap` is required. The registry returns only ready `boot` nodes with usable identity and address data from that run, so concurrent experiments cannot discover one another's bootstrap peers. `/api/v1/discovery` requires all three shown query parameters, excludes the requester, and returns configured topic participants rather than observed delivery or mesh outcomes. `/api/v1/prometheus/agent-targets` is a read-only operational endpoint used by the supplied Swarm Prometheus configuration; it omits offline Agents and Agents without a valid metrics URL.

From the repository root:

```bash
curl -X POST http://localhost:8080/api/v1/experiments \
  -H 'Content-Type: application/yaml' \
  -H "Authorization: Bearer ${KPL_API_TOKEN:-}" \
  --data-binary @examples/smoke.yaml
```

The scenario library endpoints store reusable editor inputs independently of experiment results. The list omits YAML, while individual GET, POST, and PUT responses include it. See the [scenario library guide](scenario-library.md) for the UI workflow, payloads, validation limits, and storage location.

Raw events are stored at `data/runs/<run-id>/events.jsonl`; the exact input is stored as `scenario.yaml` and experiment metadata as `experiment.json` in the same directory. In Compose/Swarm, the persistent `controller-data` volume is mounted at `/var/lib/kpl/data`, and each run's files are under `/var/lib/kpl/data/runs/<run-id>`.

Use **Download results** in the Dashboard to export a run as ZIP. **Saved results** also lists files retained from previous Controller sessions; **Refresh** reloads that list. Running experiments offer **Download snapshot**, which contains the records saved when the download starts. These exports include the full saved event log, independently of the 300-event recent buffer. See [result downloads](monitoring.md#download-experiment-results) for archive contents and collection limits.

The Agent exposes this internal operational endpoint for Controller-driven cleanup:

| Method | Agent path | Description |
|---|---|---|
| `DELETE` | `/api/v1/runs/{runId}/nodes?generation=N` | Requires an unsigned `generation`; atomically raises the run fence through N, rejects later creates at generation N or below, stops existing nodes in those generations, and returns `202 Accepted` |

This endpoint and the other Controller-to-Agent registration, heartbeat, node lifecycle, publish, and batched-telemetry endpoints use REST/JSON. They are internal cluster and operations APIs, not client-facing Controller APIs, and may change independently.

`KPL_API_TOKEN` is one shared Bearer credential for mutating KPL APIs, not a Swarm join token, Docker permission, or Grafana password. Use the same value for the Controller and every Agent; Agents pass it to their Peers automatically. It is required by the Swarm stack and optional in Compose/the CLI. An empty value disables the token check. There are no per-user roles or scoped tokens.

Enter the value in the dashboard's **Run experiment → API token** field. Running, saving, updating, or deleting through that dialog saves it in that origin's browser `localStorage` for later mutation requests; it does not expire automatically. REST clients send `Authorization: Bearer <token>`. GET reads, including state, events, SSE, and metrics, stay public. The Controller also exempts HEAD; Agents and Peers only exempt GET. The token does not encrypt HTTP traffic.

The same four job counters are present in `/api/v1/snapshot` and SSE snapshots. The dashboard displays them on each run, so active, successful, failed, and canceled background work is visible without inspecting Controller logs.
