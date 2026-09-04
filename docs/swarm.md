# Swarm Deployment and Operations Across Multiple Servers

English | [Korean](swarm.kr.md)

`compose.yaml` is for a single Docker host. For multiple servers, deploy and manage `stack.swarm.yaml` from a manager using `scripts/swarm.sh`. Swarm maintains **one Agent on each selected server**, and the Controller distributes an experiment's Peers among those Agents. Each Peer is a separate Docker container on its assigned server. Peers are not automatically rescheduled to another server as Swarm services would be.

## Before Deployment

Join your Linux servers running rootful Docker to the same Swarm. The servers need connectivity on TCP 2377 for managers, TCP/UDP 7946, and UDP 4789. Allow the VXLAN port only between trusted cluster servers. On VPNs or cloud networks, also check the underlay MTU and VXLAN overhead. [Docker overlay requirements](https://docs.docker.com/engine/network/drivers/overlay/)

If you already have a Swarm cluster, place this repository on a manager and install the Docker CLI and coreutils `timeout`. Commands run against the **active Swarm manager** selected by the current Docker context. You do not need to copy the repository to each worker or start Agents manually. Push the image to a registry accessible to every server. For a mix of amd64 and arm64 servers, use a manifest digest that includes both architectures. `stack deploy` does not build local source code. [Deploying a Swarm stack](https://docs.docker.com/engine/swarm/stack-deploy/)

```sh
docker node ls
sh scripts/swarm.sh init
# Set KPL_IMAGE in .env.swarm to the actual registry/repository@sha256:<digest>.
# Also check KPL_CONTROL_NODE_ID and KPL_AGENT_CAPACITY.
vi .env.swarm
sh scripts/swarm.sh deploy worker-a worker-b
sh scripts/swarm.sh status
```

`init` creates `.env.swarm` in the repository root with permissions `0600`. It generates the API token and Grafana password independently as 64 hexadecimal characters from 32 random bytes each. It never overwrites an existing file. The default control Node ID is the current manager. Pin it to the **exact Node ID** of the node that stores the data volumes, so Controller, Prometheus, and Grafana do not move to another server with empty local volumes. Placing an Agent on the control node makes experiments and analysis share CPU and memory.

Build and push an image containing the new code, then set its digest. For a private registry, run `docker login <registry>` on the manager first. The helper passes `--with-registry-auth` to forward the registry credentials needed by the deployment to Swarm.

The helper reads allowed `KEY=VALUE` entries in `.env.swarm` **literally**; it never uses `source` or executes shell commands from the file. It removes one matching pair of surrounding quotes, but does not expand `$VAR`, `$(command)`, or escapes. Do not use `export KEY=...` or inline comments in this file. Exported environment variables take precedence over file values. To select another file, use a command such as `sh scripts/swarm.sh --env-file /path/to/lab.env status`. The helper does not read Compose's `.env`, and direct Docker commands do not automatically load `.env.swarm` either.

`deploy worker-a worker-b` sets the `kpl.<stack>.agent=true` label on those two nodes, creates an attachable Peer overlay if needed, then runs the preflight check and deploys the stack. The default stack name is `kpl`; the helper's default Peer network is `kpl-peers`. Existing labels on other nodes are preserved. Check for address conflicts with server, LAN, VPN, and existing Docker networks, and use a unique name and Peer network for each separate stack. `deploy` returns once the deployment is submitted. Check `status` and the Controller's `/api/v1/agents` for registration before starting an experiment. The default minimum Agent count is 1; set `KPL_MIN_AGENTS=2` in `.env.swarm` to require two servers.

Agents use only the worker's Docker socket and do not require access to the manager API. Each Agent derives its ID from `{{.Service.Name}}-{{.Node.ID}}` and uses its own task's Peer overlay IPv4 address for `advertise-url` and `self-url`. A service VIP or shared DNSRR address is unsuitable as an individual Agent address because requests may reach another server. At startup, the Agent looks up its actual local image ID and uses it for Peers, keeping Agent and Peer binaries identical on that server. [Swarm global services and templates](https://docs.docker.com/engine/swarm/services/)

## Add and Remove Agents from the Manager

Use a Swarm node ID or hostname for `NODE`. These commands change Agent placement for the specified stack; they do not join servers to or remove them from the Swarm.

| Command | Behavior |
|---|---|
| `sh scripts/swarm.sh deploy [NODE...]` | Check the image, network, and nodes, then deploy or update the stack. Add Agent placement labels to the specified nodes |
| `sh scripts/swarm.sh status` | Show stack services, Agent tasks, and selected nodes |
| `sh scripts/swarm.sh add-node worker-c` | Add an Agent placement node to the existing service. Requires Linux, Ready, and Active status |
| `sh scripts/swarm.sh remove-node worker-b` | Exclude the target node from the service, confirm clean Agent shutdown, then remove its placement label |
| `sh scripts/swarm.sh remove` | Confirm Controller shutdown, then Agent shutdown and Peer cleanup, then remove stack services |

After a server is added, subsequent joins can use its capacity; existing Peers do not move. **`remove-node` stops the Peers managed by that server's Agent and therefore affects active experiments.** Labels belonging to other stacks are not changed.

A full `remove` keeps Agents running while the Controller cancels active experiments and finishes cleanup. It then requests Agent shutdown by excluding the target nodes from the Agent service's placement constraints. After verifying clean container exits, it removes placement labels and stack services. This order matters: removing labels first can cause Swarm to record even a clean exit as `Rejected`. An individual `remove-node` also removes the temporary exclusion after clearing the label.

Removal stops if cleanup cannot be verified because a node is offline, a task failed or exited abnormally, or task history is unavailable. **Retained historical task failures also block automatic removal.** Even if a later task recovered, you must manually check for leftover Peers. A service that never started also requires manual verification if it has no task history. Stops, placement exclusions, and label changes already performed are not automatically rolled back; inspect the tasks and nodes named in the error before retrying. To run an Agent again, `add-node` also clears any remaining exclusion. Experiment, Prometheus, and Grafana **data volumes and the external Peer network are preserved**.

## API Token

`KPL_API_TOKEN` is the shared Bearer token used for starting and stopping experiments, creating and deleting Peers, publishing, registration, heartbeats, and telemetry. It is separate from Swarm join tokens, Docker manager privileges, and the Grafana password. It is required for Swarm deployment and must have the same value on the Controller and every Agent. Agents automatically include it in Peer configuration, so no per-Peer setup is needed. There is no per-user or per-role permission model.

Enter the deployed `KPL_API_TOKEN` value in **Run experiment → API token** on the Controller page. This is the value in `.env.swarm` unless an environment variable overrides it. Clicking **Run** saves it in the browser's `localStorage` for that origin and uses it for subsequent Run and Stop requests. For REST requests, add the `Authorization: Bearer <token>` header. Tokens do not expire or rotate automatically.

Even with a token configured, the dashboard and GET endpoints for status, events, SSE, and metrics remain public. The Controller exempts GET/HEAD from authentication; Agents and Peers exempt GET. Leaving the token empty in CLI or Compose deployments also disables token checks on mutation requests. The token is stored in Swarm service environment variables and in each Peer's configuration JSON with permissions `0600`. It does not encrypt HTTP traffic.

## Migrate an Earlier v3 Stack

The helper manages existing services only when their names match `${KPL_STACK_NAME}_{controller,agent,prometheus,grafana}` and they carry the service label `io.kpl.application=kp2plab-v3`. It refuses to change an older v3 stack without that marker, even if the stack name matches.

First stop existing experiments and confirm Peer cleanup. Export the existing stack name, control Node ID, Peer network name, image digest, and credentials into the shell, preserving their values, then directly redeploy the latest `stack.swarm.yaml` once. Docker does not read `.env.swarm` itself, so explicitly provide the file's actual values through the environment.

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

Go process metrics describe only the Controller and Agent processes, not total Peer resource consumption. Use `docker stats`, host monitoring, or a separate container exporter to measure Peer load. Full node-status reports, Controller persistence and aggregation, central HTTP collection, and Docker CLI creation/deletion costs also limit scale. Records of terminated nodes and per-run Prometheus series are retained, so long churn experiments need measurements of both memory use and collection delays.

Swarm published ports differ from Compose's `127.0.0.1` bindings. This stack publishes ports 8080, 9090, and 3000 in host mode on the control node. Restrict access with a management-network firewall or an authenticated reverse proxy. `KPL_API_TOKEN` protects only mutation APIs, not GET requests. Anonymous Grafana access is disabled in this stack. [Publishing ports in Swarm host mode](https://docs.docker.com/engine/swarm/services/#publish-ports)

Monitoring configuration is distributed through Swarm configs, so the repository does not need to be copied to every server. Swarm configs are immutable. When configuration files change, give the config keys and their references in `stack.swarm.yaml` new versioned names to deploy new configs. Data volumes remain on the same control Node ID.

## Failures, Updates, and Shutdown

Agent updates and rollbacks use `stop-first`. Changing this to `start-first` can run two tasks with the same server and Agent ID concurrently, conflicting with cleanup of the previous Peers. Replacing an Agent stops the Peers it manages; uninterrupted upgrades of active experiments are not guaranteed. Adding a server does not move existing Peers, and only subsequent joins use the new capacity.

Draining a node affects Swarm service tasks. v3 Peers are standalone containers, so Swarm does not migrate them directly. A clean Agent shutdown removes its Peers. After an abnormal exit, restarting on the same node with the same Agent ID and Peer network name reclaims leftover containers. If the node rejoins or the stack, service, or network name changes, inspect labels on that server and separately remove Peers left under the previous ownership scope. Whole-host failures and network partitions are not hidden by recreating Peers on other servers.

The Controller is a single instance without a shared database or leader election. Keep `replicas: 1`. If the control node fails, the Controller does not automatically move to another node with empty local volumes; restore backups and set a new Node ID as needed. At startup, the Controller does not restore saved experiments or counters into memory or automatically resume active experiments. Files are retained for analysis. A Controller crash alone does not stop Peers on Agents. Before a planned update, clean up using a scenario's `stop-all` or cancellation of active runs, then check for leftover Peers. Peers from runs that are already completed but omitted `stop-all` are not cleaned up by Controller shutdown alone.

Use `sh scripts/swarm.sh remove` for full removal. Peers that are not stopped by Controller shutdown are also cleaned up during the subsequent Agent shutdown stage.

## Validation Scope

After the manager commands were added, the full Go regression suite and `test-swarm-agent.sh`, `test-check-swarm.sh`, and `test-swarm.sh` passed on Linux. The new management-command shell tests simulate Docker responses to verify removal order, isolation from other stacks, stopping on failures or missing history or query errors, retries, and configuration-file handling.

The `remove-node`, `add-node`, and full `remove` commands were also exercised with Controller and Agent test services that exit on SIGTERM in a separate Docker 29.7.2 daemon. Verification covered clean exits recorded as `shutdown / PID 0 / exit 0`, a new task after placement was restored, and zero stack services after final removal. Adding and removing a placement constraint that excluded another node preserved the running task ID. The Controller retains its replica count while placement is blocked, preventing immediate deletion of its shutdown history. Pending or canceled tasks that were never assigned to a node are excluded from shutdown verification. This validates the management commands and actual Swarm state transitions; it does not represent another full run of registry image pulls or experiments with real Peers.

On each target server, export the actual image digest as `KPL_IMAGE`, then run `docker pull "$KPL_IMAGE"` and `KPL_DOCKER_IMAGE="$KPL_IMAGE" sh scripts/check-linux.sh` to check kernel capabilities. The Linux kernel check also requires Docker Compose v2, `timeout`, and `setsid`. The manager's `deploy` command checks basic deployment conditions using `check-swarm.sh`. To run that script directly, separately pass the settings from `.env.swarm` as environment variables; its standalone default minimum Agent count is 2. These checks do not replace testing actual cross-host VXLAN communication or throughput.

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
