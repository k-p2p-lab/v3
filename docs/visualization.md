# Experiment result images

English | [한국어](visualization.kr.md)

Choose **Saved results → Images** to submit background analysis. The server reads logs, calculates graph and propagation metrics, fits distributions, and saves the result. The browser prepares white-background, 1,600px PNGs. Use **PNG ↓**, **CSV ↓**, or **Download all PNG + CSV (ZIP)**. The ZIP includes chart definitions; **Download analysis JSON** downloads the full server artifact.

Accepted server jobs continue after closing the window/browser. One worker processes up to 32 accepted jobs. Completed artifacts survive Controller restart; interrupted jobs require Retry. Reopening a completed analysis reuses it. **Analyze latest snapshot** captures later logs. An outdated analysis version is regenerated on opening. Regeneration cannot recreate missing source evidence. Configured API tokens are required to start/retry/refresh jobs. PNG conversion and cross-run calculations happen in the browser while the view is open.

## Available images

- Existing v3 latency CDF/histogram, message activity, scores and peer lifecycle.
- GossipSub/transport/Kademlia node counts, degree, non-leaf denominator degree, diameter, shortest paths, clustering, five centralities, assortativity, modularity, connected-pair fraction, degree PDF/CDF and weighted Student-t fit.
- Propagation/duplicate cumulative curves, linear/log time, combined panels, hop distributions, per-message FRT/reachability/DRC and estimated eager/lazy and unclassified paths.
- Control RPCs, entries, message-ID references, mesh transitions and reciprocal logical edges.
- Total and per-protocol sent/received stream throughput and cumulative bytes. These measure libp2p stream use, not link capacity, IP/TCP framing, retransmissions or management traffic.

Open **Message images, repeated runs and v2 comparisons**, select a message, and choose **Message images** for its latency, hop, duplicate and first-reception path images. Missing evidence and undefined statistics appear as N/A; full node IDs and membership remain in analysis JSON.

## Comparing runs

1. **Load saved results**, select runs, and assign **Series** and **Case** labels. Identical labels define repeats; low-reachability runs are retained.
2. Choose Metric, X axis and Scatter color. Baseline series compares matching cases across groups. Reference case supplies arrow origins and fixed D parameters. Empty fields use the first series/case.
3. Enter parameters such as `i=1,f=0.5,p=90` or `dLow=4,d=8,dHigh=12` when needed. Case labels `i1-f0.5-p90` and `4-8-12` are also recognized. Missing parameters are never guessed; p is a percentage.
4. Select **Analyze selected results and generate comparison images**. Accepted analyses continue after closing; generate the comparison again to reuse completed artifacts.

Images cover repeat mean/sample-SD error bars; difference, `target/(target+baseline)` and division; arbitrary metric scatter; FRT–DRC with reach color and baseline arrows; two-/three-objective Pareto; weighted scores; i/f groups; lazy contribution estimates; all four v2 duplicate regression designs; univariate/multivariate quadratic Student-t parameter models; observed/fitted/predicted distributions; and raw/peak-normalized overlays. Enter `Dlow,D,Dhigh` rows under Degree predictions for model distributions.

One repeat has no sample SD. Error propagation assumes independent errors; same-input comparisons have zero uncertainty. Zero denominators are N/A. Regressions fit case means, report rank and in-sample R², and refuse unidentifiable coefficients. Score normalization depends on the selected set; a constant dimension contributes 0.5. Lazy estimates require a matched **lazy-off baseline** and are observational differences, not causal proof.

## v2 inputs and interpretation

Import v2 metric JSONL, propagation trees, duplicate-time maps, `x_case/y/yerr` or `x/y` JSON, numeric CSV, and v3 analysis JSON locally in the browser (32MiB/file). Select the appropriate Metric before importing v2 x/y summary data. Overlay files must share units. Peak normalization changes the original probability/count meaning. The fixed historical/ER reference button renders the data stored in v2's reference script, not selected-run measurements.

The main v3 Metrics definitions remain unchanged. Additional metrics live under `research`. FRT uses non-publisher first receptions, in seconds, with envelope or clock evidence. Research reachability uses the union of subscribed non-publisher nodes at first-reception times, falling back to dispatch targets only if subscription history is absent. Propagation/duplicate curves use the same message–node cohort. `drc_per_node_count` uses mean observed graph nodes; `drc_per_target` uses receiver population.

Graphs are fresh undirected unique-neighbor observations including isolates. Distances exclude self/unreachable pairs, with connected-pair coverage reported. Snapshots have equal weight. This differs from v2's reciprocal GRAFT graph and message-conditioned observation intervals. Non-leaf denominator degree retains v2's sum of ALL degrees divided by the count of nodes with degree > 1. Student-t fits use observed probability weights directly and report convergence/parameter bounds.

The Controller saves graph edges available from current reports. Peers use trace metadata exposed by unmodified upstream libp2p: per-topic IHAVE IDs, IWANT/IDONTWANT IDs, PRUNE peer-exchange IDs, data RPC message IDs/topics, and subscriptions. `rpcObservationId` groups records from one **local RPC callback**; it is not a shared network RPC identity. `pubsub_reject` preserves the rejected wire message ID and supplied reason. See [API](api.md#detailed-peer-logs) for fields.

Eager Push/Lazy Pull are **metadata estimates**. Active GRAFT suggests eager; an IHAVE→IWANT sequence on the same peer pair/topic suggests lazy. Detailed logs match the delivered pubsub ID first; legacy count-only logs use time association within a maximum five-second lookback. This window is an analysis rule, not a protocol timeout. PRUNE clears active GRAFT; disconnection, leaving and measurement termination invalidate earlier connection evidence. Conflicting or missing evidence stays unknown. Unresolved paths retain link estimates without classifying the entire path. Even matching request IDs do not directly measure sender-queue origin.

Detailed IDs are bounded to 8,192 entries and 512KiB of hex strings per category, with completeness and omission fields; full counters are preserved. Subscription lists are capped at 8,192. Payload bodies and unavailable queue origins/packet headers are not recorded. Historical logs cannot recover missing IDs or graph edges. **Rebuild and deploy Controller and Peer images, then run new experiments** to collect detailed logs. No third_party directory or local libp2p fork is used.

Background endpoints: `POST /api/v1/analysis-jobs/{id}`, `GET` status, `/result?jobId={jobId}` full artifact, `/summary?jobId={jobId}` compact comparison artifact. Current `analysisVersion` is 3. The synchronous compatibility `/api/v1/experiments/{id}/analysis` retains its two-minute limit. No Python runtime or image service is needed.

[Experiment metrics](experiment-metrics.md) · [v2 coverage](v2-analysis-coverage.md) · [Bandwidth](bandwidth.md)
