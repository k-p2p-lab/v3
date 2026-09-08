# Complete v2 analysis coverage audit

English | [Korean](v2-analysis-coverage.kr.md)

**v3 does not reproduce every v2 analysis or visualization with identical definitions and plots.** A related chart does not establish numerical, statistical or input-format parity. Current v3 adds actual P2P bandwidth collection and visualization and displays the collected control details. Unsupported items below remain unfinished.

On 2026-09-08, the audit inspected the sibling `v2` checkout at revision `74b71090410cac1315ae08f479a95c086feeaaf6`: **40 parser output fields/families, 22 Python modules and 34 source files**, including parser inputs and batch wrappers. The [audit manifest](v2-analysis-coverage.json) records these files and their hashes. `degree_distribution-*` counts as one dynamic key family; the field count is not a feature-support percentage.

Run from the v3 repository root with the v2 checkout available as `../v2`, or supply its path:

```sh
python3 scripts/audit-v2-analysis.py
# If v2 is elsewhere:
python3 scripts/audit-v2-analysis.py --v2 /path/to/v2
```

This command checks the source keys, file set and hashes against the reviewed inventory. New or changed source requires another audit. **It does not automatically prove equivalent v3 calculations.** The UI and statistical definitions were compared separately below. Source links point to the audited v2 revision so they also work without a sibling checkout.

## All original parser outputs

Sources: [log.go](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-parser/internal/analysis/log.go), [metric.go](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-parser/internal/analysis/metric.go), [value.go](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-parser/internal/analysis/value.go), [propa.go](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-parser/internal/analysis/propa.go).

| v2 output | Current v3 | Equivalence / remaining work |
|---|---|---|
| `time` | Event/observation timestamps and relative-second axes | v2 uses log interval boundaries; v3 uses event bins and five-second observations. Sampling times differ |
| `node_count` | Peer lifecycle and eligible topology nodes | v2 JOIN/LEAVE graph nodes and v3 ready/fresh-report populations differ |
| `average_degree` | Mean unique neighbors per relationship layer | v2 uses `EdgeCount()/Nodes` on its reciprocal GRAFT/PRUNE graph. v3 combines fresh neighbor reports; graph and denominator differ |
| `average_degree_excluding_leaves` | Unsupported | v2 divides the original edge count by the number of degree > 1 nodes, or 1 if none. An induced leaf-free graph average is not equivalent |
| `diameter`, `shortest_path_length` | Unsupported | Requires graph history and v2 netkit's directionality, disconnected-graph and path-averaging conventions |
| `clustering_coefficient` | Mean local clustering | v3 uses a different graph/sample and returns N/A above its work limit. Numerical parity is not established |
| `degree_distribution-*` | Degree distribution at the selected observation | v2 viewers combine and normalize message/run average frequencies; v3 shows a selected-time distribution |
| `betweenness_centrality`, `page_rank`, `degree_centrality`, `closeness_centrality`, `eigenvector_centrality` | Unsupported | Requires source graphs and matching normalization/algorithms |
| `assortativity`, `modularity` | Unsupported | Requires matching disconnected/edgeless behavior and community rules |
| `send_ihave`, `send_iwant`, `recv_ihave`, `recv_iwant` | Control rate and control breakdown table | RPC occurrences and entry counts are separate units. v2 callback locations, time intervals and per-message aggregation differ |
| `send_graft`, `send_prune` | Control breakdown table | v2 graph-transition events and v3 wire-control observations differ |
| `logical_send_ihave`, `logical_send_iwant`, `logical_recv_ihave`, `logical_recv_iwant` | Message ID references in the table and Grafana | The reference-count unit corresponds; v2 per-message interval statistics and case plots are not implemented |
| `logical_send_graft`, `logical_send_prune` | Unsupported | Counts v2 reciprocal GRAFT processing/successful edge removal; wire-entry counts cannot replace these |
| `eager_count`, `lazy_count` | Unsupported | v3 delivery events lack `EAGER_PUSH`/`LAZY_PULL` attribution. IHAVE/IWANT alone does not establish first-receipt cause |
| `frt` | Mean/P95 latency and latency histogram/CDF | v2 uses per-message DELIVER latency statistics; v3 uses valid first receipts at stable receivers within the window. Population, seconds/ms and reducer order differ |
| `reachability` | Stable delivery bounds, starting delivery and coverage | v2 divides observed receipts by the union of graph nodes at that message's receipt times. v3 uses session/window cohorts |
| `drc` | Raw duplicate count and duplicates per eligible delivery | v2 per-message duplicate totals and the viewer's `drc / node_count` use different denominators from v3's duplicate mean |
| `eager_frt`, `eager_reachability` | Unsupported | Requires eager/lazy delivery-cause instrumentation |
| `pub_map`, `propa_map`, `dup_map`, `reach_map` | Related raw publish/deliver/duplicate/session events | No v2 map import/export or compatibility reducers; propagation and reach maps use different receipt definitions |
| `eager_propa_map`, `eager_reach_map` | Unsupported | Eager propagation source evidence is absent |

v2 computes diameter even without `--graphmetrics`; the option adds shortest-path, clustering, centralities, assortativity and modularity. Its `average/deviation/median/count` reducers apply to a message's receipt samples or the time samples contributing to that message. Deviation is population standard deviation; median is sorted `values[len/2]`. The sample unit differs from v3's whole-run mean/P95.

The audit also covers **per-message propagation trees** (`id/time/children`) and **duplicate-count maps by time since publication**. v2 attaches a child only after its `FromNodeID` parent has entered the tree, omitting receipts whose parents remain unresolved. v3 logs have PeerID/RemotePeerID and message correlation, but tree reconstruction, hop distributions and per-message duplicate-time curves are not implemented. Raw payloads, missing parents and relay paths need explicit handling. A live topology drawing is not a message propagation tree.

## All Python visualization and derived analyses

The `file_helper/process_helper/math_helper/format_helper/output_helper` helpers belong to their main-module rows. The `old/` directory is included.

| v2 module | Actual analysis | Current v3 comparison |
|---|---|---|
| [basic_graph](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/basic_graph/main.py) | Arbitrary x/y metrics, case/d/d_value selection, case-group mean/std error bars, degree distributions, JSON/images | Related fixed charts exist. No arbitrary axes, case groups, repeated-run statistics or error bars. The default reachability ≤ 0.1 exclusion rule is not ported |
| [compare_graph](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/compare_graph/main.py) | Common-case alignment, `t-b`, `t/(t+b)`, `t/b`, independent error propagation | Only mean/P95 latency and mean duplicate differences from the first run. No ratio/division, error propagation or case alignment |
| [trade-off_graph](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/trade-off_graph/main.py) + plot.py | FRT–DRC, reachability coloring, baseline deltas, 2D/three-objective Pareto, normalized weighted scores, i/f grouping | Basic latency–duplicate scatter only. Requires explicit original `i-f-p` inputs, baseline `i1-f1.0-p100` and weights 0.25/0.25/0.5; the models, coloring and grouping are absent |
| [time_vs_metric_plot.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/time_vs_metric_plot.py) | Cumulative propagation, time-dependent duplicates/node, combined plots, log-x/origin controls and numeric sample output | Related latency CDF and event rates only. v2 divides by message count × mean node_count; v3 CDF divides by latency samples. Duplicate/combined/log-x plots are absent. Exponential-fit calls are commented out in v2 and are not counted as default output |
| [calc_lazy_metric.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/calc_lazy_metric.py) | On/off ΔR, net_cov_nodes, overlap_est, net_eff, overlap_ratio, lazy_first_ratio, net_cov_ratio, lazy_first_per_iwant table | Unsupported. Requires aligned case-level node_count/IWANT/lazy-first/on-off reachability and matching denominators. These calculations alone do not establish causality |
| [dup_regression.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/dup_regression.py) | Four i/f/p-based LinearRegression models, coefficients, intercepts and R² | Unsupported. Requires metadata and explicit training samples/model rules |
| [old/graphic.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/graphic.py) | Legacy arbitrary-axis/color scatter and extraction | Fixed metrics only |
| [old/propagation.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/propagation.py) | Tree-based per-message/mean hop distributions | Unsupported |
| [old/prop_plot.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/prop_plot.py) | Propagation time/hop PDF/CDF, per-message/combined mean curves, CSV/PNG | Unsupported. v3 latency histogram/CDF use different source data and denominators |
| [old/prop_plot_graph.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/prop_plot_graph.py) | Folder/global CSV curve overlays, peak=1 normalization, long-form CSV | Unsupported. Run CDF comparison does not import or peak-normalize CSV curves |
| [old/mean_degree_ratio.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/mean_degree_ratio.py) | Legacy degree Total/Count per-entry means followed by line/file means | Unsupported; its input schema also differs from the current parser's `average_degree` |
| [old/clustering_vs_eigenvector.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/clustering_vs_eigenvector.py) | Combined metric scatter and arrows from a reference case | Unsupported |
| [old/frt_drc_graph.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/frt_drc_graph.py) | Hard-coded empirical/ER comparisons, propagated errors and two plots | Unsupported; requires fixed reference data not derived from the current run |
| [old/fitting.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/fitting.py) | Reconstruct samples from degree probabilities, fit Student-t, regress inverse degrees of freedom quadratically, predict grids | Unsupported |
| image_gen.sh / total_image_gen.sh and run.sh / run_more.sh wrappers | Batch the analyzers with specific paths and cases | v3 supports up to four selected runs and JSON/CSV/SVG export. No legacy batch/path/PNG compatibility |

## Prerequisites for the remaining port

1. **Compatibility definitions:** version the v2-compatible reducers and fix graph directionality, GRAFT/PRUNE handling, intervals, message weights, normalization, missing values and zero-denominator behavior. Reusing a similar v3 metric name can change research results.
2. **Source graphs:** `observations.jsonl` retains degree histograms/means/clustering summaries. These cannot recover historical diameter, centrality or modularity. Store relationship snapshots or replay sufficient original traces.
3. **Propagation instrumentation:** record eager/lazy first-delivery evidence and forwarding paths at the Peer. Missing historical causes cannot be filled with guesses.
4. **Repeated experiments and models:** v3 has batches/iterations, but the current screen compares individual runs. Specify case parameters, sample units, error-bar definitions, common-case alignment and model inputs before porting the derived analyses.

[Bandwidth](bandwidth.md) measures newly collected libp2p stream usage. It does not supply the missing source evidence for the unsupported analyses above.

[Current visualization](visualization.md) · [Experiment metric definitions](experiment-metrics.md)
