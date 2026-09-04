# Inspect Transport, Kademlia, and GossipSub Topology

English | [Korean](topology.kr.md)

## Read the graph

The Control Room shows a full-width interactive topology with equally sized, wedge-shaped sectors for Agents. Agent numbers match **Agent status → No.** Each Peer has a local display number inside its Agent's sector; select it to see the full node and Peer IDs. The number helps identify a Peer on screen and does not fix its position. Display numbers can be reused after a Peer departs; use the full IDs for persistent identity.

Peers settle gradually within their Agent's sector. Visible links apply spring-like forces, while repulsion and collision handling spread nearby Peers apart. Peers stay inside their assigned sector as they move. Changes to Peers or visible relationships restart the layout briefly; it stops when settled instead of adding continuous random movement. Placement is a display aid and does not change protocol behavior or experiment results.

Use the independent checkboxes above the graph:

| Layer | Default | Line | Meaning |
|---|---|---|---|
| **Kademlia** | On | Thin cyan dashed | A Peer lists the other Peer in its current DHT routing table. |
| **GossipSub mesh** | On | Thicker orange solid | A Peer reports accepted GRAFT mesh membership for the selected topic(s). |
| **Transport** | Off | Thin muted gray | An active libp2p transport connection reported by a Peer. |

Turning off a layer hides its lines and updates the layout forces to use the remaining visible relationships; it does not disable that protocol or change the experiment. **GossipSub topic** filters mesh relationships while preserving the other layers. Changing these filters can reposition Peers within their sectors. In **All topics**, a pair's mesh relations are combined into one visual line, with the topic evidence retained in the details. The summary's **Transport links** counts unique transport pairs, not the sum of all three layers.

Hover over a Peer to emphasize its visible neighbors and dim unrelated lines. Select a Peer to retain its details, including Agent/Peer display numbers, full IDs, profile, network settings, and reporting endpoints. **Clear selection** or Escape clears the selection. Use **+ / −** or Ctrl/Command + scroll to zoom, drag the graph background to pan, and **Fit** to show the whole layout. Peers support keyboard focus and Enter/Space selection.

**Pause motion** keeps existing Peer positions while leaving filters, selection, zoom, and pan available. New Peers and changes to Agent sectors still appear immediately. **Resume motion** lets the layout settle again.

With the operating system or browser's reduced-motion preference enabled, animated settling is off by default. The initial layout and relationship changes use a bounded settling calculation and display the result without intermediate animation. You can explicitly choose **Resume motion** to enable animation.

Agent sectors have equal angles, and configured capacity does not reserve empty Peer positions. Dense experiments can still look crowded; use topic filtering, layer toggles, and zoom to inspect a selected Agent or Peer.

## What each line establishes

The previous graph used `ConnectedPeers`, collected from libp2p host connections. It did not replay ADD_PEER events and could not distinguish routing-table membership from a GossipSub mesh. Transport connectivity, DHT knowledge, remote topic subscription (`TopicPeers` / PubSub `ListPeers`), and GRAFT mesh membership are different relationships.

- **Kademlia** comes from `dht.RoutingTable().ListPeers()`. Routing-table membership does not prove an active transport connection or a packet sent at that moment.
- **GossipSub** is tracked at each Peer through synchronous router callbacks. Accepted `Graft` adds membership; `Prune`, topic `Leave`, and `RemovePeer` remove it. This state is independent of the telemetry queue, event delivery order at the Controller, and the 300-row recent-event buffer. `floodsub`, `randomsub`, and disabled PubSub have no GossipSub mesh layer. Direct/fanout forwarding paths outside the GRAFT mesh are not included.
- **Transport** comes from the current host connection list. ADD_PEER events and existing connections alone never create Kademlia or GossipSub edges.

An edge is an undirected display projection of one or both endpoints' reports. `reportedBy` identifies which endpoints actually reported it; Kademlia tables and transient mesh observations can be asymmetric. A PRUNE seen by one endpoint may leave the other endpoint's report visible briefly, until that endpoint reports its new state. DHT, transport, and mesh data are not sampled at a single atomic cluster-wide instant.

## Freshness and lifecycle

Peers send status about every two seconds; Agents forward their current snapshots. Network, scheduling, and status failures add delay. Stopping, stopped, starting, and failed endpoints have no displayed relationship lines. Stopping/stopped Peer circles are hidden; failed Peers remain visible as issues.

Offline Agents and Peer status older than ten seconds are excluded from lines. To avoid comparing clocks on different servers, the Controller derives Peer-report age from two timestamps produced by the same Agent, then advances that age on its own clock after receiving the heartbeat. This is the age reported by the Agent plus time since receipt, not a bound on time spent in transit. Missing legacy timestamps cannot establish that age. `OverlayObservedAt` identifies a supported overlay snapshot; it is the Peer's clock and is not compared directly with browser/Controller time to declare staleness.

A fresh empty snapshot removes old routing/mesh memberships. Agents reject older crossed status reports and copy slices/maps before forwarding. A new snapshot therefore repairs the graph after dropped telemetry. These rules do not imply that every displayed relationship is currently transmitting or reachable.

Older Peers lacking overlay snapshots provide only Transport lines. The graph reports how many visible Peers have supplied overlay data; it does not relabel old transport connections as Kademlia. Deploy the updated image to Controller and Agents, then start new Peers to obtain all layers.

## API and retained data

`GET /api/v1/snapshot`, `GET /api/v1/network`, and the SSE snapshot carry the same typed edges. Relevant Node fields are `routingPeers` (Peer IDs), `meshPeers` (topic → Peer IDs), and `overlayObservedAt`. An example edge is:

```json
{"source":"node-a","target":"node-b","protocol":"gossipsub","topic":"kpl/demo","reportedBy":["node-a"]}
```

`protocol` is `transport`, `kademlia`, or `gossipsub`. GossipSub edges are distinct per topic in the API. Duplicate reports are merged deterministically, and unknown endpoints, ambiguous Peer identities within a run, and cross-run relationships are excluded. IDs in the graph and API are Node IDs; reported neighbor lists inside Node objects use libp2p Peer IDs.

These snapshots are live state, not a complete historical topology database. Saved `events.jsonl` retains collected GRAFT/PRUNE events, subject to telemetry loss, but ZIP exports do not yet include an exhaustive history of DHT tables or mesh snapshots. Protocol filtering and topology layout do not alter the [delivery metric definitions](experiment-metrics.md).

## Development checks

Run `go test ./...` for Peer mesh/DHT snapshots, Agent ordering/copying, Controller edges/freshness, and embedded HTML checks. Run `node --test internal/webui/topology_test.cjs internal/webui/topology-layout_test.cjs` with a modern Node.js runtime for layer/topic filters, sector layout and settling, churn, camera behavior, and DOM interaction regressions. The JavaScript tests use built-in Node.js modules and require no npm packages. These checks do not replace a browser visual review or a real multi-server experiment.
