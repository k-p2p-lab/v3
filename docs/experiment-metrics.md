# Experiment Metrics, Repetition, and Saved Results

English | [Korean](experiment-metrics.kr.md)

## Run the same scenario several times

In **Run experiment**, paste the YAML and set **Runs** beside **Run** to an integer from 1 to 100. The Controller queues all iterations and executes them sequentially, even if you close the browser. Each iteration gets a unique run ID, a separate result directory, and `batchId`, `iteration`, and `repetitions` in its metadata.

For more than one iteration, the Controller cancels or drains background jobs according to the scenario's exit policy, fences and removes that iteration's Peers, and refreshes Agent state before starting the next iteration. An execution or cleanup failure cancels all remaining iterations. **Stop batch** on any running or queued member cancels the active iteration and the remaining queue. Other independently submitted experiments can still run concurrently; keep them stopped when comparing repetitions.

The scenario YAML is unchanged in every iteration. An explicit nonzero `seed` is reused; zero or an omitted seed creates a new recorded seed per iteration. Reusing a seed repeats sampling inputs, but Docker timing, eligible populations, and network execution can still differ. Queues are not resumed after a Controller restart. Retained `queued` or `running` records are displayed as `interrupted`.

API clients can POST `application/json` to `/api/v1/experiments`:

```json
{"scenario":"version: 1\nname: repeat-example\nphases:\n  - action: wait\n    duration: 1s\n","repetitions":3}
```

The response is the first experiment; `/api/v1/snapshot` and the SSE stream include all iterations. Existing raw YAML requests still start one experiment. Mutations use the configured bearer token.

## Delivery under churn

The unit of observation is a **(run, topic, message, receiver) pair**, not a packet or a count of currently visible Peers. Only successful publications with a collected `publish` event contribute a publication cohort. A failed publish request is an operation failure, not an automatically lost message; an ambiguous RPC failure may still have published successfully if its telemetry arrives.

Immediately before each publish RPC, the Controller records `cohortCapturedAt` and the IDs of ready, online, subscribed remote Peers in `targetNodeIds`. It excludes the publisher, other runs, relay-only/topic non-subscribers, and Agents whose last report is stale. These are the Controller's last observed states; capture is not an atomic barrier across hosts. For wildcard publication, each topic gets its own cohort captured in the same inventory snapshot.

Let `C_m` be this frozen cohort for message `m`. A subsequent join is outside `C_m`; a departure after capture stays inside it. Let `D_m` be cohort members with a collected application delivery. The displayed delivery ratio is:

```text
expectedDeliveries = sum_m |C_m|
eligibleDeliveries = sum_m |D_m|
delivery ratio = eligibleDeliveries / expectedDeliveries
```

This is weighted by intended receiver pairs, not the unweighted mean of per-message percentages. For example, one reached pair out of one target, followed by one out of three targets, gives `2 / 4 = 50%`, not 66.7%. A cohort member that departs before delivery remains an unmet target. Removing its circle from the topology does not improve the ratio. No targets means **N/A**, not 0% or 100%.

The observation window is the collected run history up to the current snapshot or ZIP boundary. There is no built-in per-message deadline or assertion of eventual loss. During execution, missing pairs can be in flight, disconnected, departed, or missing telemetry. Use the same warm-up, publication, and collection periods when comparing runs; the churn example waits 30 seconds after its final round. Delayed telemetry can update a completed result. This delivery ratio is not netem's packet-loss probability or the probability of delivery conditional on surviving long enough to receive.

## First delivery latency

For each successful cohort pair, latency is the first successful `Subscription.Next` receipt time minus the timestamp placed in the envelope when the publisher prepares that publication after acquiring its local publish gate. This includes serialization, PubSub processing, the network, and subscriber queue delay. It excludes Controller-to-Agent dispatch latency and waiting for the publisher's local publish gate.

Only the earliest application delivery for that pair contributes. Sender-local delivery, later duplicate deliveries, late joiners, and unknown cohorts do not contribute. Raw payloads contain no application send timestamp, so their delivery ratio is available but their latency is **N/A**. Negative clock-skew samples are excluded and counted in `invalidLatencySamples`; a positive clock offset cannot be detected this way. Synchronize all host clocks with chrony/NTP before comparing one-way latency.

The UI and `metrics.json` report the arithmetic mean and nearest-rank P95 across valid successful pairs, plus `latencySamples`. Missing deliveries are not zero milliseconds and do not enter the latency distribution. Always report the delivery ratio beside latency: quick survivors alone do not demonstrate reliable delivery.

## Average duplicate messages

An additional copy is a GossipSub `RawTracer.DuplicateMessage` observation at a receiver. It is a PubSub message cache hit, not TCP retransmission, repeated IHAVE advertisement, duplicate payload bytes, or a second application delivery. Let `dup(m,r)` be additional observed copies at a successful cohort receiver. Then:

```text
eligibleDuplicates = sum_m sum_{r in D_m} dup(m,r)
averageDuplicates = eligibleDuplicates / eligibleDeliveries
```

Zero duplicates at a successful receiver contributes zero; no successful receivers means **N/A**. Publisher-returned copies and copies at late joiners remain in the total `duplicates` event count but are excluded from this mean. For instance, three successful receiver pairs with 0, 1, and 5 additional copies give `6 / 3 = 2` duplicates per successful delivery. This is not a byte-overhead ratio or duplicates per publication.

Envelope publish/delivery/duplicate events share the application message ID. Raw events use `pubsub-<hex native message ID>`, so two separate publications of identical bytes remain separate. `fields.pubsubMessageId` preserves the original ID in either encoding. The wire format and PubSub's origin-plus-sequence ID algorithm are unchanged. Each new event has an `eventId`, generated once and retained across telemetry retries; the Controller persists and counts it once. Older events without an event ID cannot be reliably deduplicated as transport retries.

## Scope, export, and monitoring

New metric summaries carry `definition: "dispatch-cohort-v1"` to identify these rules.

- The web metric cards select the latest running experiment, or the most recently started terminal experiment if none is running. Queued iterations do not replace its metrics. The selected run is shown above the cards.
- Metric accumulation uses the whole run observed by this Controller process. The 300-event recent feed and its 40 visible rows only limit event display. The compact index grows with publications, receiver pairs, and event IDs until result deletion or process restart.
- `published`, `delivered`, and `duplicates` retain overall event counts; `delivered` includes local and late-join delivery events. Use `eligibleDeliveries / expectedDeliveries` for the delivery ratio. Neither total events nor duplicate counts are packet counts.
- ZIP downloads add **`metrics.json`**, rebuilt from exactly the same captured `events.jsonl` prefix. This also works after a Controller restart and deduplicates retained event IDs. Unknown historical cohorts are reported in `unscopedPublications` and excluded from delivery ratio/latency/duplicate averages; a legacy `targetNodes` count alone is insufficient. Rebuilding cannot recover lost telemetry. A malformed event log produces an incomplete/failed ZIP rather than fabricated metrics.
- Prometheus exposes `kpl_delivery_expected_pairs`, `kpl_delivery_reached_pairs`, `kpl_delivery_ratio`, `kpl_delivery_duplicate_copies`, and `kpl_delivery_duplicates_per_reached_pair`, grouped by run. Ratio series are absent when their denominator is zero. Grafana's new cohort panels use only the Run filter and weight multiple runs by receiver pairs.
- `kpl_propagation_latency_seconds` uses the same successful remote pairs, grouped by run, receiving Agent, and topic. Grafana estimates whole-run P50/P95/P99 from histogram buckets; the web P95 is exact nearest rank. Late telemetry can backfill/correct the histogram, so query it directly rather than applying `rate`/`increase`. Traffic event counters retain their usual rate semantics. Older pre-upgrade latency series included local deliveries; compare runs created with the same metric definition.
- Peer telemetry queue loss, Agent/Controller failures, and shutdown before flushing can bias all measurements. Keep reported telemetry loss alongside results. Packet loss under `scope: all` can also affect the measurement channel itself.

## Topology and deletion

Agent numbers match the **No.** column in **Agent status**. The browser retains its ID-to-number mapping in local storage; another browser may assign different numbers. Real Agent IDs and hostnames remain in the table and tooltips. Topology circles and incident edges exclude `stopping` and `stopped` Peers. Failed Peers remain visible as issues; inventory/history is retained.

The [topology guide](topology.md) explains independent Kademlia/GossipSub/Transport checkboxes, topic filters, zoom, selection, and status freshness. These display controls do not change protocol behavior or measurement cohorts.

Choose **Delete** in **Saved results**, inspect the run name/ID, and confirm. This permanently removes that run's saved scenario, metadata, and event log plus its live metric index. It does not stop Peers or erase previously scraped Prometheus/Grafana history. Running/queued runs, members of an active batch, and results with an active ZIP download are protected. Deleting an old interrupted result is not a Peer cleanup operation.

The endpoint is `DELETE /api/v1/results/{id}`: 204 deleted, 404 missing, 409 busy, and 401 for a missing/invalid configured token. A small persistent deletion marker rejects late telemetry that would otherwise recreate the result. Keep deletion markers with the Controller data when migrating or backing it up.

## References and design choice

[Vyzovitis et al., GossipSub: Attack-Resilient Message Propagation, §7.3](https://research.protocol.ai/publications/gossipsub-attack-resilient-message-propagation-in-the-filecoin-and-eth2.0-networks/vyzovitis2020a.pdf) and the [GossipSub v1.1 evaluation report](https://research.protocol.ai/publications/gossipsub-v1.1-evaluation-report/vyzovitis2020.pdf) evaluate propagation distributions/tail latency, loss, and duplicate deliveries as separate quantities. They do not prescribe the frozen churn cohort used here. That cohort, pair weighting, observation boundary, and duplicates-per-success definition are explicit KPL experimental design choices, allowing departures to remain observable rather than changing the denominator after the outcome.

The [official PubSub message identification specification](https://github.com/libp2p/specs/blob/master/pubsub/README.md#message-identification) explains origin-plus-sequence IDs and the implications of content-based deduplication. KPL correlates telemetry without changing that protocol behavior.
