# Saved-result visualization and comparison

English | [한국어](visualization.kr.md)

Open **Saved results → Analyze** or **Analysis** in the Dashboard. Add up to four runs with **Saved run → Add run**. The first selection is the baseline for mean/P95 latency and duplicate differences. **Remove** changes the selection; **Refresh analysis** reloads it. Active runs are analyzed as explicit snapshots, without automatic refresh. If a request fails or contains invalid chart data, an existing valid snapshot stays visible with its original analysis timestamp and an error message. Use **Refresh analysis** to retry; an error is not a newly measured zero.

## Charts

| Chart | Meaning |
|---|---|
| Propagation latency CDF | Cumulative fraction of eligible first remote latency samples, not network reachability |
| Latency / duplicate trade-off | One point per run: mean latency versus observed duplicates per eligible delivered pair |
| Latency histogram | Eligible sample counts by latency bin center in milliseconds |
| Message event rate | Raw publish/deliver/duplicate events per second in each time bin |
| GossipSub control rate | Send/receive/drop event counts across IHAVE, IWANT, IDONTWANT, GRAFT and PRUNE; one wire RPC can carry multiple types |
| P2P stream throughput / Cumulative P2P stream transfer | Reported send/receive usage in kbit/s and KiB, selectable by negotiated protocol |
| GossipSub control breakdown (table) | Per-Agent/direction/type RPC occurrences, entries, message ID references and peer-exchange records |
| Peer lifecycle | Ready, starting, stopping, failed and freshly reporting peer counts |
| Average degree / Clustering coefficient | Mean unique neighbor count and mean local clustering for a relationship layer |
| Degree distribution | Fraction of peers at each degree in the observation selected by the slider |
| Peer score / Negative peer scores | Mean/min/max observer-to-peer scores and fraction of negative score reports |

**Inspect run** chooses detailed charts. **Observation group** filters topology and score observations; **Relationship layer** filters topology charts only. Message and latency charts cover the entire run. Degrees include neighbors in other groups, merge repeated neighbors across topics, and use only fresh reporting peers. Local clustering is zero for degrees below two; unknown observations remain null gaps, rather than zeros.

The summary also shows starting-cohort delivery, stable coverage bounds and departed pairs alongside the primary delivery ratio. Compare measurement definitions and `deliveryWindows` in the summary alongside latency sample, invalid, pending and unknown counts. The API reuses the existing v3 metric accumulator, including stable subscribed-session membership and on-time delivery windows. Legacy logs retain their own measurement definition. Raw and invalid latency samples are excluded. Delivery bounds describe missing evidence, not confidence intervals. Differences between runs with different definitions or windows do not establish a controlled performance difference.

## Recording and exports

The Controller records `runs/<id>/observations.jsonl` every five seconds during a run and at finalization, independently of browser clients. The optional file travels with the result ZIP and survives Controller restarts. Older results without observations still have event and latency charts; unavailable historical topology and scores are not reconstructed. Shorter changes and unreported relationships cannot be recovered.

- **Download analysis JSON** exports the currently selected snapshots, including metrics, distributions and observations for all groups and layers.
- **Download summary CSV** exports run metrics, definitions, windows and analysis timestamps; unavailable values are blank cells.
- Each chart's **SVG** button downloads a vector image including its title and legend.
- **Download results / Download snapshot** exports original scenario, event and observation files in the existing ZIP format.

The response limits charts to 360 event bins, 1,440 observations, approximately 200 CDF points and 30 histogram bins. Observation endpoints are retained. Binning preserves event totals; metric summaries and histogram counts use all eligible samples. If exact triangle work exceeds 250,000 neighbor pairs for a layer, clustering is null. Original files are not downsampled. Display rounding does not alter exported JSON values.

## API

```http
GET /api/v1/experiments/{id}/analysis
```

The JSON response contains `version: 1`, `result`, `asOf`, `eventBytes`, `eventCount`, `untimedEvents`, `metrics`, `latencyCDF`, `latencyHistogram`, `timeline`, `binSeconds`, `observations`, `observationCount`, `bandwidthTimeline` and `bandwidthBinSeconds`. Distribution points are `{x, y}`; CDF x is milliseconds and y is a fraction from 0 to 1. Timeline fields are counts per bin; the UI divides them by `binSeconds`. `asOf` is the captured file-boundary timestamp and also determines measurement-window maturity.

Responses use 404 for missing/deleted results, 405 for unsupported methods, 422 for unreadable records, and 504 for analysis timeout. The Controller analyzes one result at a time with a two-minute request deadline including queue time. Event identities remove telemetry retries and records for another run are ignored. Analysis reads the full saved event log independently of the 300-event recent buffer. Persistence locks are released after capturing files so the scan does not block event appends.

## v2 reference

The implementation adapts the purposes of v2 `kpl-viewer/time_vs_metric_plot.py`, `basic_graph`, `compare_graph` and `trade-off_graph` to v3's event schema and metric definitions. It uses a Go Controller API and embedded JavaScript/SVG, with no Python, Matplotlib, CDN or Prometheus requirement for this screen. It does not import raw v2 files or port experiment-specific regression/Pareto scoring models.

[Metric definitions](experiment-metrics.md) · [Monitoring and retention](monitoring.md)

## Bandwidth and the full v2 audit

[Bandwidth measurement](bandwidth.md) adds actual libp2p throughput and cumulative bytes here and in Grafana. The **GossipSub control breakdown** table separates per-Agent/direction/type RPCs, entries, message IDs and peer-exchange records.

**Full v2 parity is not implemented.** See the [40-field and complete Python module audit](v2-analysis-coverage.md) for gaps, differing semantics and required source data.
