# Swarm Deployment and Operations Across Multiple Servers

English | [Korean](swarm.kr.md)

`compose.yaml` is for a single Docker host. For multiple servers, deploy and manage `stack.swarm.yaml` from a manager using `scripts/swarm.sh`. Swarm maintains **one Agent on each selected server**, and the Controller distributes an experiment's Peers among those Agents. Each Peer is a separate Docker container on its assigned server. Peers are not automatically rescheduled to another server as Swarm services would be.

## Before Deployment

Start with Linux servers already joined to the same Swarm, using rootful Docker without userns-remap. Place this repository on a manager and install the Docker CLI, coreutils `timeout`, and the usual POSIX shell utilities. Run commands from the repository directory against the **active Swarm manager** selected by the current Docker context. Workers do not need a copy of the repository or a manually started Agent.

Swarm membership does not create an image registry. You need a registry reachable from the manager and every target node, with authentication and HTTPS trust already configured where required. The helper does not install a registry or change the Docker daemons' global TLS/insecure-registry settings. For example, `172.20.4.171:5000/kpl-v3:v3` can replace the example image below if that is your existing registry; an HTTP registry requires the relevant Docker daemons to be configured for it beforehand.

Servers need TCP 2377 for managers, TCP/UDP 7946, and UDP 4789 between cluster nodes. Keep VXLAN access within the trusted cluster and check underlay MTU/VXLAN overhead on VPNs or cloud networks. The smoke experiment also requires kernel support for network shaping. [Docker overlay requirements](https://docs.docker.com/engine/network/drivers/overlay/)

For the distributed smoke test, use at least two Agent nodes. With one manager and two workers, `--workers` keeps experiments off the manager. With one manager and one worker, use `--all` instead so both nodes run an Agent; the manager then shares CPU and memory with experiments. Keep the same user and Docker context throughout. If Docker requires sudo, use `sudo sh scripts/swarm.sh ...` consistently, including `init`, `login`, `publish`, and deployment commands.

## First Deployment, Experiment, and Download

### 1. Configure and publish the image

```sh
sh scripts/swarm.sh nodes
sh scripts/swarm.sh init KPL_IMAGE=registry.example.com/kpl-v3:v3 KPL_AGENT_CAPACITY=20 KPL_MIN_AGENTS=2
sh scripts/swarm.sh config
# Only when the registry requires authentication:
sh scripts/swarm.sh login
sh scripts/swarm.sh publish
```

Replace the image reference with your registry and repository. `init` validates the supplied settings, selects the current manager as the control node by default, and creates a private `.env.swarm` without overwriting an existing file. It generates separate API and Grafana credentials. If configuration already exists, use `configure KEY=VALUE...` to change it. `config` shows effective settings with credentials redacted.

`login` infers the registry from `KPL_IMAGE` and runs an interactive login under the same account as the helper. `publish` builds this repository and pushes the configured image tag. Skip `publish` when that image is already published. The default build targets the native platform; for mixed architectures, use a capable existing Buildx builder and publish every target architecture, including the manager:

```sh
sh scripts/swarm.sh publish --platforms linux/amd64,linux/arm64
```

The helper does not install a builder or CPU emulation. Native execution requires an image for the node's architecture. [Docker multi-platform builds](https://docs.docker.com/build/building/multi-platform/)

### 2. Deploy and open the control pages

```sh
# Use --all instead for a cluster with one manager and one worker.
sh scripts/swarm.sh deploy --workers
sh scripts/swarm.sh check
sh scripts/swarm.sh status
sh scripts/swarm.sh access
sh scripts/swarm.sh credentials
```

`deploy` resolves the image, labels the selected nodes, creates the attachable Peer overlay if needed, runs deployment checks, and submits the stack. It returns before every service and Agent is ready. `check` reruns the configuration/network/placement preflight using the loaded settings; it does not verify cross-host traffic or install kernel rules on every worker. `status` shows Docker service/task/node state, not Controller registration.

Open the Controller URL printed by `access`. Ports are published on the configured **control node**, which may differ from the manager running the command. From a machine that can reach that address, wait until the Control Room shows **at least 2 Online Agents**. In **Agent status**, consider only `online` rows: **Peers** displays occupied / capacity, and the sum of capacity minus occupied must be **at least 6**. The summary's **Available slots** can include offline Agents, so use the table for this check. A running Swarm task alone does not prove registration. If startup fails, inspect `sh scripts/swarm.sh logs agent` or `sh scripts/swarm.sh logs controller`.

`credentials` explicitly prints the configured API token and Grafana login in plaintext. Use its API token in the Controller and its separate Grafana username/password for the Grafana URL from `access`. An existing Grafana volume retains its administrator password; changing the configuration alone does not reset that password.

### 3. Run the distributed smoke experiment

```sh
# Prints examples/swarm-smoke.yaml. An explicit FILE prints that scenario instead.
sh scripts/swarm.sh scenario
```

In the Control Room, choose **Run experiment**, replace the entire **YAML scenario** field with the printed YAML, enter the API token, and click **Run**. The web form's default scenario is a different smoke test; it does not automatically load the Swarm example.

The example creates one boot Peer and five workers. Workers use 25ms delay, 2ms jitter, and 0.5% loss. After readiness and a 20-second settling period, it publishes fifty messages with `payloadSize: 4096`, waits one minute for collection, and runs `stop-all`. With equally sized, otherwise idle Agents, balanced placement spreads the Peers across them. Confirm Peers appear on different Agents and publish/deliver events are present. Readiness confirms process initialization, not GossipSub mesh convergence, and message delivery counts depend on actual conditions.

For analysis, open the Grafana URL, sign in, and select this experiment in **Run** on **KP2PLab Experiment Analysis**. Use **Agent** and **Topic** to compare servers and traffic. Control Room summary figures use recent events; Grafana counters and saved event logs cover a different scope. See the [monitoring guide](monitoring.md).

### 4. Download results, then remove services

Wait for the run to finish and check that its Peers are stopped. With no other experiments running, Agent occupancy should return to zero. To cancel an active run, use its **Stop** button and wait for cleanup.

Choose **Download results** on the run or in **Saved results**. The ZIP contains `scenario.yaml`, `experiment.json`, `events.jsonl`, and `export.json`. It includes the full saved event log, independently of the 300-event recent buffer, but not the Prometheus/Grafana time-series database. **Download snapshot** on a running experiment contains only records saved at the download boundary. [Download contents and limits](monitoring.md#download-experiment-results)

Download before removing the services, since removal also takes the web pages offline:

```sh
sh scripts/swarm.sh remove
```

Removal waits for Controller shutdown, then Agent shutdown and Peer cleanup, before deleting stack services. Experiment, Prometheus, and Grafana volumes and the external Peer network remain. Reuse the same configuration when redeploying retained volumes. **Saved results** then exposes retained runs again; a previously running record appears as `interrupted` and is not resumed. That state does not establish Peer cleanup. Failed or unavailable nodes can block automatic removal; see the cleanup rules below.

For a longer experiment with workers continuously joining and expiring, follow [random publishing during churn](swarm-churn-publish.md). It uses two stable bootstrap Peers, repeated publisher selection, and a final collection/cleanup stage.

## Configuration and Network

Use helper commands for ordinary configuration changes:

```sh
sh scripts/swarm.sh configure KPL_AGENT_CAPACITY=20 KPL_MIN_AGENTS=2
# Optional: select an unused subnet before the Peer overlay is created.
sh scripts/swarm.sh configure KPL_PEER_SUBNET=10.11.0.0/24
sh scripts/swarm.sh config
```

`configure` validates changes and atomically replaces the configuration file while preserving existing credentials unless explicitly changed. It saves the requested changes to existing file settings, without saving unrelated environment overrides. Configuration changes take effect in services on the next `deploy`; they do not reconfigure running Peers. The file has mode `0600`. Unless supplied explicitly, `init` generates each credential from 32 random bytes, encoded as 64 hexadecimal characters. `config` redacts secrets; only `credentials` intentionally displays them.

The default stack name is `kpl`, and the helper's default Peer network is `kpl-peers`. `KPL_PEER_SUBNET` is optional; if omitted, Docker assigns the subnet when creating the overlay. If supplied, it must not overlap server, LAN, VPN, or other Docker networks. An existing network with a different subnet causes deployment to fail; changing the setting does not resize that network. A larger subnet such as `/16` is accepted but does not establish support for larger experiments; see the capacity limits below. Use distinct stack and Peer network names for separate deployments.

Keep `KPL_CONTROL_NODE_ID` pinned to the **exact Node ID of the node retaining the data volumes**, so Controller, Prometheus, and Grafana do not move to another server with empty local volumes. `nodes` lists the nodes for selection. The helper's default minimum Agent count is 1; the procedure above explicitly sets `KPL_MIN_AGENTS=2`. That preflight minimum does not replace checking live Agent registration and free slots in the UI.

The helper reads allowed `KEY=VALUE` entries in `.env.swarm` **literally**, without sourcing it or executing shell expressions. One matching pair of outer quotes is removed; variables, command substitutions, and escapes are not expanded. Exported environment variables take precedence over file values, so check `config` if a file change seems ineffective. `sudo` may filter environment variables; use the same account throughout and keep routine settings in the helper's file. Select another file with `sh scripts/swarm.sh --env-file /path/to/lab.env config`. Compose's `.env` is not loaded.

Agents use only their local node's Docker socket and do not require access to the manager API. Each Agent derives its ID from `{{.Service.Name}}-{{.Node.ID}}` and uses its own task's Peer overlay IPv4 for `advertise-url` and `self-url`. A service VIP or shared DNSRR address cannot identify an individual Agent reliably. At startup, an Agent resolves its actual local image ID for Peer creation, keeping Agent and Peer binaries identical on that node. [Swarm global services and templates](https://docs.docker.com/engine/swarm/services/)

## Update an image using the same tag

After updating the source in this repository, finish active experiments, then run:

```sh
sh scripts/swarm.sh publish
sh scripts/swarm.sh deploy
sh scripts/swarm.sh status
```

Keep the same tag in configuration. To switch from an old digest or to another tag, first run:

```sh
sh scripts/swarm.sh configure KPL_IMAGE=registry.example.com/kpl-v3:v3
```

Then use the same `publish` → `deploy` sequence. `publish` requires a tag, not a digest. For mixed architectures, use `publish --platforms linux/amd64,linux/arm64` instead of the native build and include every target architecture plus the manager.

Each deployment pulls the configured tag, resolves the registry digest returned by that pull, and pins **this deployment only** without changing `.env.swarm`. **No manual SHA replacement is needed after a push.** A failed pull stops deployment instead of using a stale local tag. Explicit `repository@sha256:<digest>` references remain supported for deployment when an immutable version is wanted. [Docker pull and digest pinning](https://docs.docker.com/reference/cli/docker/image/pull/#pull-an-image-by-digest-immutable-identifier)

Timeout settings are in seconds: `KPL_IMAGE_BUILD_TIMEOUT=1800`, `KPL_IMAGE_PUSH_TIMEOUT=600`, and `KPL_IMAGE_PULL_TIMEOUT=300` by default. The separate push timeout applies to native publishing; Buildx combines build and push under the build timeout. The tag-resolution pull uses the manager daemon's native platform, ignoring `DOCKER_DEFAULT_PLATFORM` for that pull, and retains Docker's reported manifest/index digest. Replacing Agents can stop their Peers; after deployment, wait for Agent registration before starting another experiment.

## Add and Remove Agents from the Manager

Use Swarm node IDs or hostnames for an explicit `NODE` list. For `deploy`, `add-node`, and `remove-node`, you may instead supply one of these selectors. They are mutually exclusive and cannot be mixed with an explicit node list. These commands change Agent placement for the specified stack; they do not join servers to or remove them from the Swarm.

| Selector | Nodes considered |
|---|---|
| `--all` | All nodes, including managers |
| `--all-excluding-self` | All nodes except the manager reached through the current Docker context |
| `--workers` | Nodes whose Swarm role is `worker`; managers are excluded |

**“Self” is the `.Swarm.NodeID` reported by the Docker daemon selected through the current context.** It is not `KPL_CONTROL_NODE_ID` or the hostname of the shell's machine. In a multi-manager cluster, `--all-excluding-self` can select other managers. Use `--workers` to exclude every manager.

For automatic `deploy`/`add-node` selection, only Linux, Ready, Active nodes are included; other matching nodes are reported as skipped. For automatic `remove-node` selection, candidates are first limited to nodes carrying this stack's `kpl.<stack>.agent=true` label. Unavailable or ineligible removal targets cause failure rather than being skipped, preserving cleanup verification. An empty selection fails.

Deployment and addition add to the existing placement labels; they do not remove labels from nodes outside the selection. Bare `deploy` reuses the existing labels. A selector is evaluated only when its command runs: it does not create a persistent rule that automatically enables Agents on future Swarm members.

```sh
# Add all currently eligible worker-role nodes, including newly joined workers.
sh scripts/swarm.sh add-node --workers
# After finishing experiments on these nodes, stop this stack's selected Agents.
sh scripts/swarm.sh remove-node --all-excluding-self
```

Makefile targets forward `NODES` unchanged, so `make swarm-add-node NODES='--workers'` runs the same selection.

| Command | Behavior |
|---|---|
| `sh scripts/swarm.sh deploy [NODE...]` | Deploy or update using a node list or one selector; without either, reuse existing labels. Pull and resolve the image tag or use an explicit digest |
| `sh scripts/swarm.sh status` | Show Docker stack services, Agent tasks, and selected nodes; does not check API readiness |
| `sh scripts/swarm.sh nodes` | List current Swarm nodes |
| `sh scripts/swarm.sh init [KEY=VALUE...]` | Create validated private configuration and credentials; never overwrite an existing file |
| `sh scripts/swarm.sh configure KEY=VALUE...` | Save validated changes atomically, preserving other settings and credentials |
| `sh scripts/swarm.sh config` / `credentials` | Show effective configuration with secrets redacted / explicitly show API and Grafana credentials |
| `sh scripts/swarm.sh login` | Log in interactively to the registry inferred from the configured image |
| `sh scripts/swarm.sh publish [--platforms CSV]` | Build and push the configured tag; optional platforms use the existing Buildx builder |
| `sh scripts/swarm.sh check` | Rerun deployment preflight with loaded settings |
| `sh scripts/swarm.sh access` | Print control-node URLs; does not test reachability or service readiness |
| `sh scripts/swarm.sh logs [COMPONENT]` | Show the last 100 timestamped lines; controller by default, or agent, prometheus, grafana |
| `sh scripts/swarm.sh scenario [FILE]` | Print YAML for the web form; defaults to the distributed smoke example and does not submit it |
| `sh scripts/swarm.sh add-node worker-c` | Add an Agent placement node to the existing service. Requires Linux, Ready, and Active status |
| `sh scripts/swarm.sh remove-node worker-b` | Exclude the target node from the service, confirm clean Agent shutdown, then remove its placement label |
| `sh scripts/swarm.sh remove` | Confirm Controller shutdown, then Agent shutdown and Peer cleanup, then remove stack services |

After a server is added, subsequent joins can use its capacity; existing Peers do not move. **`remove-node` stops the Peers managed by that server's Agent and therefore affects active experiments.** Labels belonging to other stacks are not changed.

A full `remove` keeps Agents running while the Controller cancels active experiments and finishes cleanup. It then requests Agent shutdown by excluding the target nodes from the Agent service's placement constraints. After verifying clean container exits, it removes placement labels and stack services. This order matters: removing labels first can cause Swarm to record even a clean exit as `Rejected`. An individual `remove-node` also removes the temporary exclusion after clearing the label.

Removal stops if cleanup cannot be verified because a node is offline, a task failed or exited abnormally, or task history is unavailable. **Retained historical task failures also block automatic removal.** Even if a later task recovered, you must manually check for leftover Peers. A service that never started also requires manual verification if it has no task history. Stops, placement exclusions, and label changes already performed are not automatically rolled back; inspect the tasks and nodes named in the error before retrying. To run an Agent again, `add-node` also clears any remaining exclusion. Experiment, Prometheus, and Grafana **data volumes and the external Peer network are preserved**.

## API Token

`KPL_API_TOKEN` is the shared Bearer token used for starting and stopping experiments, creating and deleting Peers, publishing, registration, heartbeats, and telemetry. It is separate from Swarm join tokens, Docker manager privileges, and the Grafana password. It is required for Swarm deployment and must have the same value on the Controller and every Agent. Agents automatically include it in Peer configuration, so no per-Peer setup is needed. There is no per-user or per-role permission model.

Use `sh scripts/swarm.sh credentials` to read the effective `KPL_API_TOKEN`, and enter the value applied by the deployment in **Run experiment → API token** on the Controller page. Configuration changes require deployment before the services use the new token. Clicking **Run** saves it in the browser's `localStorage` for that origin and uses it for subsequent Run and Stop requests. For REST requests, add the `Authorization: Bearer <token>` header. Tokens do not expire or rotate automatically.

Even with a token configured, the dashboard and GET endpoints for status, events, SSE, and metrics remain public. The Controller exempts GET/HEAD from authentication; Agents and Peers exempt GET. Leaving the token empty in CLI or Compose deployments also disables token checks on mutation requests. The token is stored in Swarm service environment variables and in each Peer's configuration JSON with permissions `0600`. It does not encrypt HTTP traffic.

## Migrate an Earlier v3 Stack

**Legacy exception:** skip this section for a new deployment. These direct Docker operations are only a one-time migration of an older, unmarked stack; routine operations above use the helper. The helper does not bypass its ownership checks.

The helper manages existing services only when their names match `${KPL_STACK_NAME}_{controller,agent,prometheus,grafana}` and they carry the service label `io.kpl.application=kp2plab-v3`. It refuses to change an older v3 stack without that marker, even if the stack name matches.

First stop existing experiments and confirm Peer cleanup. Export the existing stack name, control Node ID, Peer network name, image reference, and credentials into the shell, preserving their values, then directly redeploy the latest `stack.swarm.yaml` once. Docker does not read `.env.swarm` itself, so explicitly provide the file's actual values through the environment.

```sh
# Run on the manager after exporting the existing deployment values above.
docker node update --label-add "kpl.${KPL_STACK_NAME}.agent=true" worker-a
docker node update --label-add "kpl.${KPL_STACK_NAME}.agent=true" worker-b
docker stack config --compose-file stack.swarm.yaml >/dev/null
docker stack deploy --with-registry-auth --compose-file stack.swarm.yaml "$KPL_STACK_NAME"
sh scripts/swarm.sh status
```

The new placement uses stack-specific `kpl.<stack>.agent` labels instead of the shared `kpl.agent` label. If the existing Peer network is named `kpl-swarm-peers`, keep that name in `.env.swarm`. Switching to the helper's default name changes the ownership scope used for existing Peers. Use the helper for subsequent deployments, additions, and removals.

## Distribution and Capacity

| Setting | Placement behavior |
|---|---|
| `placement: balanced` (default) | Choose the online Agent with the lowest occupancy-to-capacity ratio. Break ties by Agent ID |
| `placement: random` | Use the seed to choose among online Agents with available capacity |
| `placement: single-agent` | Pin one join execution to one selected Agent. Each repeat selects again. Wait if that Agent is full rather than switching servers |
| `agentId` | Pin placement to the specified Agent. Use an ID from `/api/v1/agents` for experiments pinned to a physical server |
| `parallelism` | Number of concurrent jobs in a join with `parallel: true`. This is separate from the server count or Swarm replica count |

Capacity is a Peer-count admission limit, not a CPU or memory reservation or a cgroup limit. Changing the Agent's Swarm resource limits does not apply those limits to its sibling Peer containers. The supplied stack applies the same `KPL_AGENT_CAPACITY` to every Agent. Separate Agent service groups with different per-server capacities are outside this helper's supported scope. Choose capacity based on measured CPU, RAM, file-descriptor, conntrack, and Docker daemon load.

The Controller serializes reservations across concurrent experiments. An Agent holds a slot from Docker creation **until deletion is confirmed**, and failed cleanup continues to occupy capacity. The Controller does not reuse a slot based only on a DELETE response. An Agent is excluded from new placement after more than 10 seconds without a report, such as during a network partition. Checks prevent reports from an earlier Agent process or out-of-order reports from overwriting current node and capacity data. A new Agent with the same ID can register after the previous process's online validity window expires.

Docker creation admission and the subsequent configuration copy, startup, and address inspection each receive a separate 45-second budget. This prevents time spent waiting for the daemon to create a container from consuming its startup budget. Shorter deadlines and cancellation from the original request still apply. If a busy server repeatedly exceeds these limits, lower join `parallelism` and capacity and investigate disk and daemon load.

Docker recommends `/24` addressing for ordinary overlay networks. Service tasks and endpoints consume addresses in addition to Peers, so do not set `server count × capacity` equal to the full address count. The default capacity of 20 is a starting point, not a performance guarantee. Extending v2's `/16` configuration or merely increasing capacity does not establish support for thousands of Peers. The current configuration uses a single shared Peer overlay; hundreds or thousands of Peers require a design for reachability across multiple networks and separate load testing. [Swarm overlay size limitations](https://docs.docker.com/engine/swarm/networking/#overlay-network-size-limitations)

Peers advertise only the overlay IPv4 address on their route to the Agent through libp2p. This keeps `docker_gwbridge` addresses that are unreachable from other servers out of bootstrap and Identify data. tc delay and loss apply to outbound traffic in the Peer namespace; extra latency from physical NICs, VXLAN, and server load remains. Comparing one-way latency between hosts also requires synchronized clocks.

## Monitoring and Operations

Prometheus resolves `tasks.agent` on the private monitoring overlay to scrape each Agent separately. Discovery runs every five seconds to pick up added servers and restarted tasks. Grafana provisions the existing KP2PLab dashboard automatically. Check these together:

- The number of `up{job="kpl-agent"}` targets and the actual Agent task count
- The Controller's `kpl_agent_active_nodes`, `kpl_agent_capacity`, and `kpl_agent_up`
- Each Agent's `kpl_local_cleanup_pending` and `kpl_local_telemetry_queue_events`
- Actual experiment ready/start times, failure counts, and per-server CPU, memory, and network usage

Go process metrics describe only the Controller and Agent processes, not total Peer resource consumption. Use host monitoring or a separate container exporter to measure Peer load. Full node-status reports, Controller persistence and aggregation, central HTTP collection, and Docker CLI creation/deletion costs also limit scale. Records of terminated nodes and per-run Prometheus series are retained, so long churn experiments need measurements of both memory use and collection delays.

Swarm published ports differ from Compose's `127.0.0.1` bindings. This stack publishes ports 8080, 9090, and 3000 in host mode on the control node. Restrict access with a management-network firewall or an authenticated reverse proxy. `KPL_API_TOKEN` protects only mutation APIs, not GET requests. Anonymous Grafana access is disabled in this stack. [Publishing ports in Swarm host mode](https://docs.docker.com/engine/swarm/services/#publish-ports)

Monitoring configuration is distributed through Swarm configs, so the repository does not need to be copied to every server. Swarm configs are immutable. When configuration files change, give the config keys and their references in `stack.swarm.yaml` new versioned names to deploy new configs. Data volumes remain on the same control Node ID.

## Failures, Updates, and Shutdown

Agent updates and rollbacks use `stop-first`. Changing this to `start-first` can run two tasks with the same server and Agent ID concurrently, conflicting with cleanup of the previous Peers. Replacing an Agent stops the Peers it manages; uninterrupted upgrades of active experiments are not guaranteed. Adding a server does not move existing Peers, and only subsequent joins use the new capacity.

Draining a node affects Swarm service tasks. v3 Peers are standalone containers, so Swarm does not migrate them directly. A clean Agent shutdown removes its Peers. After an abnormal exit, restarting on the same node with the same Agent ID and Peer network name reclaims leftover containers. If the node rejoins or the stack, service, or network name changes, inspect labels on that server and separately remove Peers left under the previous ownership scope. Whole-host failures and network partitions are not hidden by recreating Peers on other servers.

The Controller is a single instance without a shared database or leader election. Keep `replicas: 1`. If the control node fails, the Controller does not automatically move to another node with empty local volumes; restore backups and set a new Node ID as needed. At startup, the Controller does not restore live experiment state or counters into memory or automatically resume active experiments. Retained files are available through **Saved results** in the Control Room and the [result download API](monitoring.md#download-experiment-results), including after a restart. A previously running record is displayed as `interrupted`, which does not confirm Peer cleanup. A Controller crash alone does not stop Peers on Agents. Before a planned update, clean up using a scenario's `stop-all` or cancellation of active runs, then check for leftover Peers. Peers from runs that are already completed but omitted `stop-all` are not cleaned up by Controller shutdown alone.

Use `sh scripts/swarm.sh remove` for full removal. Peers that are not stopped by Controller shutdown are also cleaned up during the subsequent Agent shutdown stage.

## Validation Scope

After the manager commands were added, the full Go regression suite and `test-swarm-agent.sh`, `test-check-swarm.sh`, and `test-swarm.sh` passed on Linux. The new management-command shell tests simulate Docker responses to verify removal order, isolation from other stacks, stopping on failures or missing history or query errors, retries, and configuration-file handling.

The `remove-node`, `add-node`, and full `remove` commands were also exercised with Controller and Agent test services that exit on SIGTERM in a separate Docker 29.7.2 daemon. Verification covered clean exits recorded as `shutdown / PID 0 / exit 0`, a new task after placement was restored, and zero stack services after final removal. Adding and removing a placement constraint that excluded another node preserved the running task ID. The Controller retains its replica count while placement is blocked, preventing immediate deletion of its shutdown history. Pending or canceled tasks that were never assigned to a node are excluded from shutdown verification. This validates the management commands and actual Swarm state transitions; it does not represent another full run of registry image pulls or experiments with real Peers.

For normal Swarm preflight, use `sh scripts/swarm.sh check`; the helper loads configuration and invokes the deployment checker. The separate host-local kernel diagnostic described in the [Linux guide](linux-deployment.md#prepare-and-start-the-server) is optional troubleshooting, not a requirement to copy this repository to every worker. It needs additional local tooling and tests a disposable container. Neither deployment preflight nor a local kernel check replaces the distributed smoke test, actual cross-host VXLAN communication, or throughput measurements.

Run the [distributed experiment example](../examples/swarm-smoke.yaml) on at least two servers. Check that ready Peers appear on different Agents, advertised addresses belong to the Peer overlay, and publish/deliver events occur on both servers. The example creates one boot Peer and five workers, requiring total capacity of at least 6. Then increase the target Peer count gradually while recording readiness delays, failure rates, distribution, and resource usage. Also verify exclusion of stale Agents, leftover-container cleanup, and Prometheus target replacement after network partitions or Agent restarts.

### Earlier Stack Validation: 2026-09-04

The record below predates the manager CLI `scripts/swarm.sh`. It is not validation of the new CLI's complete `init/deploy/add-node/remove-node/remove` path on an actual cluster.

The environment consisted of **two isolated Docker daemons on the same WSL2 Linux kernel**. Docker 29.7.2 manager and worker daemons were configured with an actual attachable overlay. The existing host's Swarm state was not changed. This tested placement, VXLAN, and service discovery between two daemons, not physical-server NICs, MTUs, failures, or throughput at scale. The same local image was preloaded into both daemons; registry authentication and image pulls require separate validation.

| Item | Result |
|---|---|
| Linux code validation | Full Go suite passed. The Agent package and Swarm shell regression tests passed again after the final timeout-budget fix |
| Swarm deployment | Two global Agents, one Controller, one Prometheus, and one Grafana ran successfully. Preflight passed on the actual manager |
| Node distribution | Six Peers were split evenly across two Agents with capacity 4: three Peers per Agent, all ready and connected |
| Peer addresses | Every advertised address was a single Peer overlay IPv4 address on TCP 20000. No gateway bridge addresses were included |
| Network and publishing | Workers used 25ms delay, 2ms jitter, and 0.5% loss. Published a 4096-byte payload 50 times and recorded 300 deliveries, 150 per Agent |
| Monitoring | Both Prometheus Agent targets, the Controller, and Prometheus itself were UP. Grafana dashboard UID `kpl-experiments` and the datasource API connection were verified |
| Task replacement | After an image update, node-based Agent IDs were preserved while task IPs changed and Prometheus targets were refreshed |
| Clean shutdown | Run `run-20260904T040249Z-34d32bbb9559adbba6519a558c6cc107` completed; both Agents reported activeNodes 0, and each daemon had zero remaining Peer containers |

Publish and delivery counts come from the Controller's cumulative `kpl_events_total`, rather than snapshot summaries based on only the 300 most recent events. The first load run exposed a shared 45-second creation-and-startup timeout that terminated a Peer. The results above were obtained after separating those stage budgets.
