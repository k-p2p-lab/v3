# Experiment result images

English | [Korean](visualization.kr.md)

Choose **Saved results → Images** to submit background analysis. The server reads logs, calculates graph and propagation metrics, fits distributions, and saves the result. The browser prepares white-background, 1,600px PNGs. Use **PNG ↓**, **CSV ↓**, or **Download all PNG + CSV (ZIP)**. The ZIP includes chart definitions; **Download analysis JSON** downloads the full server artifact.

Accepted server jobs continue after closing the window/browser. At most 32 queued/running jobs are admitted, with one computation at a time. Completed artifacts survive Controller restart; interrupted jobs require Retry. Reopening a completed analysis reuses it. **Analyze latest snapshot** captures later logs. An outdated analysis version is regenerated on opening. Regeneration cannot recreate missing source evidence. Configured API tokens are required to start/retry/refresh jobs. PNG conversion and cross-run calculations happen in the browser while the view is open.

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

## Check definitions and collection evidence

Main Metrics and the additional `research` values use different populations, windows and aggregation even when their names match. Research reachability is conditioned on receipt observation times; raw messages can also have a research log-time estimate when timing evidence exists. See [saved-result research metrics](experiment-metrics.md#saved-result-research-metrics) for definitions, units, samples and graph normalization. Do not group different definitions as repeats under one Series/Case. Imported v2 values retain their original definitions and are not automatically converted.

Eager Push/Lazy Pull colors and contribution values are metadata estimates. Zero classified eager/lazy receipts can coexist with unknown receipts, so report the unclassified count. The [estimation rules](experiment-metrics.md#eager-push-and-lazy-pull-estimates) define message-ID/GRAFT/IHAVE/IWANT evidence and conflicts; [detailed Peer logs](api.md#detailed-peer-logs) define available fields and omission limits.

Reanalysis cannot recover message IDs, graph edges or session evidence absent from historical records. Deploy the current image to Controller/Agents and run new Peers to collect that version's logs, following [Swarm updates](swarm.md#failures-updates-and-shutdown). The current implementation uses upstream libp2p observations and needs no Python runtime or image-generation service.

## Analysis persistence and API

The [background analysis API](api.md#background-analysis) defines job state/version, source snapshot time and full/compact responses. The source boundary is captured after the job acquires its computation slot, not at queue admission. Completed work keeps that boundary; use **Analyze latest snapshot** to include later-arriving logs.

The server retains completed analysis JSON. The browser generates PNGs, CSVs and comparison ZIPs while the view is open. The **Download results** source ZIP, **Download analysis JSON**, and **Download all PNG + CSV (ZIP)** are different artifacts. See [analysis and image retention](monitoring.md#analysis-and-image-retention) for contents and persistence.

[Experiment metrics](experiment-metrics.md) · [v2 coverage](v2-analysis-coverage.md) · [Bandwidth](bandwidth.md)
