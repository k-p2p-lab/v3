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

## Delivery under churn: session-window-v1

The primary metric asks: **of the remote application sessions subscribed throughout a fixed delivery window, how many received the message before its deadline?** It is conditional on continued subscription, not the probability that every original recipient survives churn and receives the message. Display it beside the starting-cohort delivery ratio, stable coverage, and measurement quality.

The unit is a **(run, topic, message, receiver session) pair**. A process restart creates a new session even if a node ID is reused. Only successful publications with a collected `publish` event enter the analysis. A failed request is an operation failure; an ambiguous RPC response can still correspond to a successful publication whose telemetry later arrives.

### Fixed window and subscription evidence

Each `publish` phase accepts `deliveryWindow`, a Go duration greater than zero and no longer than one hour. The default is `10s`. Choose it before running the experiment and keep it fixed when comparing results.

`t` is the publisher's actual application publication timestamp, recorded after acquiring its local publish gate. The deadline is `d = t + deliveryWindow`. Controller dispatch time and the browser's current Peer count do not define the cohort.

Peer telemetry records `measurement_start` after actual subscription setup, including `sessionId` and the exact `fields.subscribedTopics`. The same process emits `measurement_checkpoint` every two seconds and `measurement_stop` on an orderly session end. These are evidence from instrumented application sessions, not a physical uptime monitor. A checkpoint interval is not a delivery grace period.

For each message, the Controller reconstructs these sets from recorded sessions in the same run and topic, excluding the publisher:

- **Known starting cohort K:** subscription began at or before `t`, and a checkpoint or stop proves that the same session continued at least until `t`. A session starting after `t`, or confirmed ended at or before `t`, is excluded.
- **Stable cohort S:** a member of K with a checkpoint or stop at or after `d`, and no subscription-session stop before `d`.
- **Departed D:** a member of K with a stop before `d`. It is excluded from both the stable numerator and denominator, **even if it received the message before leaving**. It remains in the known starting cohort.
- **Continuity unknown C:** a member of K for which the log proves neither departure before `d` nor continuity through `d`.
- **Publication-time availability unknown:** a candidate session started before `t`, but the recorded evidence does not prove that it was still present at `t`. It remains outside K and is reported separately as measurement quality.

Every known starting pair has exactly one continuity outcome, so `K = S + D + C`. A forced process termination can leave either kind of unknown tail; it is not automatically classified as a departure or a delivery failure.

The Agent can also record `measurement_terminated` after confirming process/container termination. When the Controller receives this Agent event, it replaces the Agent-host timestamp with its own receipt time and retains the original in `fields.sourceTimestamp`. The receipt time is a cross-host comparable **upper bound on termination time**, not the exact death time and not a receipt checkpoint. It can exclude a session definitely dead before publication, or establish departure before the deadline when a separate Peer checkpoint already proves presence at publication. It cannot prove an otherwise unknown tail survived through the deadline. Docker churn still uses forced removal; a forced kill does not run the Peer's graceful telemetry drain.

Transport disconnection, GRAFT/PRUNE, mesh removal, stale inventory, and an offline Agent are not subscription-session stop evidence. Removing a circle from the topology cannot improve the metric. Actual later session evidence can revise an earlier unknown classification.

The subscription window is `[t, d)`: a stop exactly at `d` qualifies as continuous for this window. A receipt exactly at `d` is on time. A session ended at `t` is outside the starting cohort.

### Maturity, delivery, and unknown observations

A message remains `pending` until `d`; it does not enter finalized pair totals before then. For mature messages, success is the first collected application delivery at or before `d` by that receiver session. Copies after the deadline are late, not on-time successes. Sender-local delivery and subsequent joins never contribute.

Each Peer event has a source `sessionId`, increasing `sequence`, and a retry-stable `eventId`. A missing receipt becomes a confirmed miss only when the source sequence prefix from `measurement_start` is complete through the session evidence covering the window. A gap or invalid timestamp ordering leaves the receipt **unknown**. Retries may fill the gap later. A passed deadline does not prove that all telemetry has arrived, so finalized results can still be corrected by later batches.

For known stable pairs, let S be their count, R the on-time successes, U the unknown receipts, and F the confirmed misses:

```text
S = R + U + F
stable delivery lower bound = R / S
stable delivery upper bound = (R + U) / S
```

When U is zero, the bounds coincide. These are logical bounds from missing observations, **not statistical confidence intervals**. A zero denominator is N/A. Unknown pairs are not removed to make the ratio look better. Availability uncertainty is a separate issue: these bounds describe known stable sessions, not an unknown total population.

The known-starting-cohort ratio retains confirmed departures and continuity-unknown pairs. Its numerator includes on-time receipts by departed sessions. If Rk is the on-time successes and Uk is the receipt-unknown count in K, the displayed bounds are `Rk / K` and `(Rk + Uk) / K`.

Stable coverage also has logical bounds:

```text
stable coverage lower bound = S / K
stable coverage upper bound = (S + C) / K
```

Both supporting metrics are available whenever `K > 0`; only an empty known starting cohort makes them **N/A**. Publication-time availability-unknown pairs remain outside K, and `measurementIncomplete` remains visible as a quality warning. Neither condition suppresses a useful result for the evidence that is present. The bounds are conditional on the known cohort and do not claim coverage of an unseen population. Known unscoped sequence streams trigger `measurementIncomplete`; completely invisible sessions or telemetry streams cannot be detected from the received log alone. A false flag therefore does not certify perfect observation of every real Peer.

In `metrics.json`, `publicationAvailabilityUnknownPairs` counts candidates outside K, `continuityUnknownPairs` is C, `stableCoverage` is the lower bound, and `stableCoverageUpperBound` is the upper bound. `availabilityUnknownPairs` remains the aggregate of the two unknown categories for compatibility.

The Controller's historical `targetNodeIds` dispatch snapshot is retained only as an additional audit hint: a listed recipient with no valid session-start record marks measurement incomplete. It does not add that recipient to the denominator or determine its actual publication-time availability. Sessions absent from both instrumentation and this snapshot can still be invisible.

For example, all ten initial subscribers are observed. Two leave before the deadline, one after receiving. Eight remain subscribed for the whole window, and seven of those receive on time. With complete telemetry:

```text
stable conditional delivery = 7 / 8 = 87.5%
starting-cohort delivery    = 8 / 10 = 80%
stable coverage            = 8 / 10 = 80%
```

The early successful departure is excluded from both stable counts. Keeping its success while removing the other departed receiver would make eligibility depend on the outcome.

Totals are **receiver-pair weighted**: sum successes and denominators across mature messages, then divide. They are not an unweighted mean of message percentages. One success out of one eligible receiver followed by one out of three gives `2 / 4 = 50%`, not 66.7%. Changing the window changes both on-time success and the population that survives it; compare the same window and report coverage alongside the primary result.

### Clock and collection limits

Publication, subscription, checkpoint, stop, and receipt timestamps come from different hosts. At startup, each Peer spends at most five seconds obtaining up to seven Controller health samples. Fast failures are spaced 250ms apart instead of exhausting every attempt immediately. It selects the minimum-RTT sample and shifts measurement timestamps to the Controller clock using the request midpoint. Failure does not prevent the Peer from becoming Ready: it proceeds unsynchronized and retries every five seconds. After synchronization it refreshes every 30 seconds.

A successful sample remains trusted for two minutes. If refreshes keep failing past that age, the Peer retains the last offset for timestamp continuity but stops attaching trusted clock metadata. A later successful sample restores it. While a sample is trusted, events record `clockBasis`, `clockOffsetMs`, and the half-RTT `clockUncertaintyMs`; an envelope carries the publisher uncertainty, and a receiver reports the combined uncertainty as `latencyUncertaintyMs`.

The Controller uses that bound when a one-way point estimate crosses zero or the delivery deadline. A slightly negative estimate whose uncertainty still permits a causal, on-time receipt counts as delivery, while the negative value is excluded from the latency histogram. A bound entirely before publication is invalid; a bound wholly after the deadline is late; a bound that straddles the deadline is unknown. Periodic sampling limits drift while the Controller is reachable, but midpoint estimates and temporary outages do not replace host time synchronization. Keep chrony/NTP enabled on every host, especially for long churn runs. Instrumented session continuity does not prove continuous physical connectivity.

Peer telemetry uses bounded retries and an orderly shutdown drain. Agent telemetry uses backpressure for a full queue rather than acknowledging and discarding that batch; its orderly shutdown allows Peer cleanup before a bounded final drain. Queue overflow, exhausted shutdown time, process failure, and Agent/Controller failures can still lose observations. Sequence tracking distinguishes some gaps from confirmed misses; it cannot recreate missing data or prove the existence of a wholly unobserved session. `scope: all` can impair the measurement channel itself. Keep loss counters and incomplete/unknown indicators with every result.

## First delivery latency

The latency samples use mature, stable, on-time remote receiver pairs. For each pair, take the first successful `Subscription.Next` receipt time minus the envelope timestamp prepared after the publisher acquires its local publish gate. This includes serialization, PubSub processing, network transit, and the subscriber queue. It excludes Controller-to-Agent dispatch and waiting for that local gate.

Raw payloads have no embedded application send timestamp: they contribute to session-window delivery ratios but latency remains **N/A**. Local receipts, later deliveries, departed/late-joining receivers, and unavailable latency samples do not enter the distribution. Negative point estimates are excluded and counted in `invalidLatencySamples`, even when bounded uncertainty lets the receipt count as an on-time delivery.

The UI and `metrics.json` report arithmetic mean, nearest-rank P95, and `latencySamples`. Missing or unknown receipts are not zero milliseconds. These latencies are conditional on observed on-time success and stable subscription; always show delivery bounds and coverage beside them.

## Average duplicate messages

An extra copy is a receiver's GossipSub `RawTracer.DuplicateMessage` observation, a PubSub message-cache hit. It is not a TCP retransmission, repeated IHAVE advertisement, byte count, or a second application delivery.

The duplicate average divides observed extra copies within the same delivery window at stable, on-time successful receiver pairs by the number of those successful pairs. Zero-copy successful pairs contribute zero; no successful pairs means N/A. Local copies, copies at departed/late-joining sessions, and copies outside the window remain in overall event counts but not this mean. For successful pairs with 0, 1, and 5 extra copies, the mean is `6 / 3 = 2`. Missing duplicate telemetry can still lower this observed average.

Envelope events share the application message ID. Raw events use `pubsub-<hex native message ID>`, distinguishing separate publications of identical bytes. `fields.pubsubMessageId` preserves the native ID. Wire format and PubSub's origin-plus-sequence message-ID algorithm are unchanged; the telemetry source sequence is a different counter. Event IDs survive retries so the Controller stores and counts each event once.

## GossipSub control traffic

KPL observes the control metadata attached to GossipSub `SEND_RPC`, `RECV_RPC`, and `DROP_RPC` traces. For every RPC and every control type present in it, the Peer emits one `send_*`, `recv_*`, or `drop_*` event. The supported suffixes are `ihave`, `iwant`, `idontwant`, `graft`, and `prune`. A mixed RPC containing both IHAVE and IWANT therefore contributes to both series.

Three counts have deliberately different units:

- **RPCs** count envelopes containing a given control type. Summing across types can count one mixed envelope more than once.
- **Entries** count repeated protobuf control entries inside those RPCs.
- **Message IDs** count ID references inside IHAVE, IWANT, and IDONTWANT entries. Repeated references remain repeated; this is neither a unique-message count nor a delivery count.

PRUNE peer-exchange records are counted separately. IHAVE, GRAFT, and PRUNE carry topic fields, so raw events retain sorted `fields.topics`, `fields.topicEntryCounts`, and, for IHAVE, `fields.topicMessageIdCounts`. The top-level event topic stays empty because one RPC can span several topics and IWANT/IDONTWANT have no wire topic. Prometheus therefore exposes these counters by run, Agent, direction, and control type without a topic label. The downloaded `metrics.json` contains the same per-Agent breakdown rebuilt from its exact `events.jsonl` boundary.

`send` means the local outbound queue accepted the RPC; it does not prove a stream write or remote receipt. `recv` is an inbound observation before later router admission and flood-limit decisions. Adding both directions can count the two endpoint observations of one transfer and must not be treated as a unique wire-RPC total. `drop` is a local pre-send discard such as queue saturation or an oversized RPC, not a packet dropped by `netem`. The existing plain `graft` and `prune` events describe local mesh transitions and remain separate from wire `send_graft`/`recv_graft` and `send_prune`/`recv_prune` events.

This keeps v2's useful split between RPC occurrences and logical ID references while fixing its mixed-RPC `if/else-if` loss and adding IDONTWANT and dropped RPCs. IDONTWANT also depends on GossipSub v1.2 support and message size. With the pinned defaults it is normally produced only for data at least 1,024 bytes; use a larger payload or lower `gossipsub.params.iDontWantMessageThreshold` when an experiment is intended to exercise it.

## Scope, export, and monitoring

New summaries use `definition: "session-window-v1"`. A publication records `fields.measurementDefinition` and `fields.deliveryWindow`. The stored session evidence, publication and receipt times, and source sequences allow the same calculation from the raw log.

- The web cards select the latest running experiment, or the most recently started terminal experiment. Queued iterations do not replace its metrics; the selected run is named above the cards.
- Accumulation covers the whole observed run, independent of the 300-event recent feed and its 40 visible rows. The index grows with sessions, publications, receiver pairs, and event IDs until deletion or process restart.
- `published`, `delivered`, and `duplicates` remain overall event counts. Overall delivery includes local and late-join receipts; dividing it by publications is not a delivery ratio or packet-loss rate.
- ZIP `metrics.json` is rebuilt from exactly the captured `events.jsonl` prefix, including after a Controller restart. Maturity uses the manifest's fixed `exportedAt` boundary; use that same timestamp when reproducing the calculation. `deliveryWindows` lists the observed valid window settings. Later telemetry is outside that download. Incomplete logs yield unknown/incomplete results; malformed event logs fail the export rather than fabricate metrics.
- Prometheus `kpl_window_*` gauges expose stable and known-starting-cohort counts, delivery bounds, coverage bounds, pending publications, departures, continuity unknowns, publication-time availability unknowns, and measurement quality by run. Grafana's session panels use only the Run filter and combine runs by receiver pairs. Starting delivery and coverage are N/A only when the known starting denominator is zero, not when quality warnings are nonzero.
- `kpl_window_propagation_latency_seconds` groups the same successful pairs by run, receiving Agent, and topic. Grafana estimates whole-run quantiles from histogram buckets; web P95 is exact nearest rank. Late telemetry can revise these gauges and histogram buckets: query directly, not with `rate`/`increase`. Traffic event counters keep normal rate semantics.

### Historical results

`dispatch-cohort-v1` used the Controller's ready/online subscriber IDs immediately before dispatch, kept later departures in the denominator, and had no message deadline. Older data may have only a target count and no identifiable recipient cohort. These are **legacy definitions**. Do not reinterpret them as continuous-session measurements or mix their old `kpl_delivery_*`/`kpl_propagation_latency_seconds` series with the new window series. Historical raw events remain available, and legacy/unscoped publication counts identify excluded records in mixed data. Downloading cannot add session evidence that was never recorded. New Grafana session panels do not display legacy-only results.

## Topology and deletion

Agent numbers match the **No.** column in **Agent status**. The browser retains its ID-to-number mapping in local storage; another browser may assign different numbers. Real Agent IDs and hostnames remain in the table and tooltips. Topology circles and incident edges exclude `stopping` and `stopped` Peers. Failed Peers remain visible as issues; inventory/history is retained.

The [topology guide](topology.md) explains independent Kademlia/GossipSub/Transport checkboxes, topic filters, zoom, selection, and status freshness. These display controls do not change protocol behavior or measurement cohorts.

Choose **Delete** in **Saved results**, inspect the run name/ID, and confirm. This permanently removes that run's saved scenario, metadata, and event log plus its live metric index. It does not stop Peers or erase previously scraped Prometheus/Grafana history. Running/queued runs, members of an active batch, and results with an active ZIP download are protected. Deleting an old interrupted result is not a Peer cleanup operation.

The endpoint is `DELETE /api/v1/results/{id}`: 204 deleted, 404 missing, 409 busy, and 401 for a missing/invalid configured token. A small persistent deletion marker rejects late telemetry that would otherwise recreate the result. Keep deletion markers with the Controller data when migrating or backing it up.

## References and design choice

The original [HyParView report, §2.5 and §5.2, printed pp. 5 and 9–10](https://www.dpss.inesc-id.pt/~ler/reports/dsn07-leitao.pdf) defines reliability over active nodes and tests dissemination after injecting failures. It illustrates a survivor-population question, not a rule for retrospectively removing only unsuccessful departed receivers.

[Pongthawornkamol et al., ICAC 2013, §2.2 and §3.2.2–3, printed pp. 249 and 251](https://www.usenix.org/system/files/conference/icac13/icac13_pongthawornkamol.pdf) define reliability in terms of interested events delivered before a deadline and use event-flow weighting. Their broker/link failure model differs from receiver-session churn.

The continuous-subscription window, session evidence, pair weighting, unknown bounds, and duplicates-per-success rules above are explicit KPL design choices, not a standard dictated by either paper. Conditioning on a whole fixed window selects more persistent sessions; starting-cohort results and coverage expose that selection. These definitions are independent of v2 reproduction.

The [official PubSub message identification specification](https://github.com/libp2p/specs/blob/master/pubsub/README.md#message-identification) explains native message IDs. KPL correlates instrumentation without changing that protocol behavior.
