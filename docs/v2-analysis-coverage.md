# v2 analysis and visualization coverage

English | [한국어](v2-analysis-coverage.kr.md)

This inventory covers 40 parser fields/families and 34 Go, Python and shell sources from the supplied v2 revision `74b71090410cac1315ae08f479a95c086feeaaf6`. [The manifest](v2-analysis-coverage.json) records hashes. Run `python3 scripts/audit-v2-analysis.py --v2 ../v2` to detect changes or omissions; this is not a numerical equivalence test.

The following visualization families are available through **Saved results → Images** and its comparison tools. Main Metrics keep their session/delivery-window definitions; additional analysis uses `research.definition = v2-corrected-observations-v1`. **Different source observations and statistical definitions can produce different values from v2.** Missing measurements remain N/A, and unresolved propagation origins remain unknown.

## Parser outputs

| v2 family | v3 representation and conditions |
|---|---|
| `node_count`, `average_degree`, `average_degree_excluding_leaves` | Protocol/group timelines and summaries. The non-leaf denominator statistic is ALL degrees divided by the number of nodes with degree > 1 |
| `diameter`, `shortest_path_length` | Computed from saved edges. Excludes self/unreachable pairs and reports connected-pair coverage |
| `clustering_coefficient`, `assortativity`, `modularity` | Mean local clustering, degree correlation and seeded Louvain modularity; undefined statistics remain N/A |
| Five centralities: betweenness, PageRank, degree, closeness, eigenvector | Normalized global/group means for saved graph observations |
| `degree_distribution-*` | Mean per-snapshot degree probability, PDF/CDF and weighted Student-t fit |
| `frt`, `reachability`, `drc` | Per-message first-reception latency, reach and duplicate counts; summaries over message means. DRC/graph-node and DRC/target are separate |
| `eager_count`, `lazy_count`, `eager_frt`, `eager_reachability` | Estimates from GRAFT/IHAVE/IWANT and message IDs. Unclassified counts remain visible; eager FRT requires classified receipts with timing evidence |
| `pub_map`, `propa_map`, `reach_map`, `eager_propa_map`, `eager_reach_map` | `research.messages`: publication, receipt, parent, hop, population and evidence; propagation/estimated-eager curves; JSON/CSV export |
| `dup_map`, `time` | Per-message duplicate cumulative curves and time axes; raw duplicate events remain in events.jsonl |
| `send_ihave`, `recv_ihave`, `send_iwant`, `recv_iwant` | Separates RPC occurrences containing a control type, entries and referenced IDs |
| Logical IHAVE/IWANT send/receive fields | Advertised/requested ID counts, distinct from unique IDs or delivered bytes |
| `send_graft`, `send_prune`, `logical_send_graft`, `logical_send_prune` | Mesh transitions and reciprocal logical-edge creation/removal. Wire RPC counts have separate `_rpc` names |
| v3 additions | IDONTWANT, control drops, unknown paths, lifecycle/score summaries, measured total/per-protocol libp2p stream bandwidth |

## Python and batch visualization

| v2 source | v3 tool |
|---|---|
| `basic_graph/*` | Series/Case repeat means and sample-SD bars, arbitrary metrics and D conditions, degree probabilities |
| `compare_graph/*` | Matched-case difference, target/(target+baseline), target/baseline and error propagation |
| `trade-off_graph/*` | FRT–DRC scatter, reach color, reference arrows, two/three-objective Pareto, weighted score, i/f groups and p filter |
| `time_vs_metric_plot.py` | Propagation/duplicate cumulative curves, linear/log time and combined panels |
| `calc_lazy_metric.py` | Eight observational contribution/overlap/efficiency estimates against a matched lazy-off baseline |
| `dup_regression.py` | All four duplicate regression designs, coefficients/rank/in-sample R², observed/fitted comparison |
| `old/graphic.py` | Metric JSONL and x/y imports; arbitrary metric, axis and color scatter |
| `old/propagation.py`, `old/prop_plot.py` | Per-message first-reception paths, latency/hop distributions, mean cumulative receiver counts and increments over time/hops |
| `old/prop_plot_graph.py` | CSV/JSON curve overlays at raw or peak-normalized scale |
| `old/mean_degree_ratio.py` | Total/Count imports and mean snapshot degree probabilities |
| `old/clustering_vs_eigenvector.py` | Two-metric scatter and reference-case arrows |
| `old/frt_drc_graph.py` | Three fixed historical/ER reference charts from v2 constants, accessed separately from selected experiments |
| `old/fitting.py` | Weighted Student-t MLE; df/location/scale trends; univariate/multivariate quadratic models and predictions for supplied D values |
| `image_gen.sh`, `total_image_gen.sh`, `run*.sh` | Submit selected server analyses and export all PNG/CSV/chart definitions as ZIP. Does not execute the old Python CLI/path layout |

Imports support up to 32MiB/file: metric JSONL, propagation tree JSON/JSONL, duplicate-time maps, x_case/y/yerr or x/y JSON, numeric CSV, and v3 analysis JSON. Arbitrary v2 internal data shapes and original directory/file naming are not automatically reproduced. See [visualization](visualization.md) for operation.

## Definitions and source evidence

- Research FRT uses non-publisher first receptions in seconds. Within-sample deviation is population SD; even medians average the two middle values. Repeat error bars use sample SD, unavailable for one repeat.
- Research reachability uses the union of subscribed non-publisher nodes at receipt times, or publication time when no receipts exist. Dispatch targets are a fallback only when subscription history is absent. Propagation and duplicate ratios use the same message–node cohort.
- v2 reciprocal GRAFT graphs and v3 fresh undirected neighbor observations differ. Saved snapshots have equal weight. Group centralities average the group's nodes using full-graph paths. Historical summary-only archives cannot recover missing edges.
- Eager/Lazy is estimated, not direct sender-cause measurement. New logs match delivered IDs to IHAVE→IWANT; old logs use a maximum five-second same-peer/topic association. Conflicts, missing evidence and cyclic paths stay unclassified. Disconnection, leaving and measurement termination invalidate prior evidence. JSON/CSV preserve link estimates and their evidence.
- Student-t uses observed probability weights directly. Plots distinguish discrete degree probabilities from continuous density and expose convergence failures/parameter bounds. No synthetic 60,000-sample reconstruction or arbitrary fallback parameters are used. Unidentifiable regressions remain N/A.
- New detailed IDs and graph edges require updated Controller/Peer images and subsequent experiments. There is no third_party fork, payload-body collection or unavailable sender-queue instrumentation. [Bandwidth](bandwidth.md) measures actual stream use, not physical link capacity.

Validation is in `internal/controller/analysis_*_test.go`, `internal/peer/tracer_test.go`, `internal/webui/research_test.cjs` and `result_images_test.cjs`. It checks small reference graphs, probability/statistical models and UI behavior; it does not assert identical results across all v2 datasets.
