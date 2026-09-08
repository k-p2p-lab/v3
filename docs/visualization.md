# Experiment result images

English | [Korean](visualization.kr.md)

Choose **Saved results → Images** to open graph images for that result. Select an image or **PNG ↓** to download it. Each white-background PNG is 1,600 pixels wide and includes a title, run identity, snapshot time, axes and legend.

The image set contains first-delivery latency CDF, latency histogram and message activity. When the records exist, it also includes GossipSub control traffic, total P2P stream throughput, Peer scores by group and average GossipSub mesh degree by group. Groups appear as separate lines in the same image. Older results without topology/score history do not receive invented observations.

Active results represent the snapshot captured when opened. Closing the dialog cancels pending work; reopening generates images from a new snapshot. **Retry** handles request or image-generation failures. The multiple-run selection, baseline comparison, filters and JSON/CSV/SVG export workspace have been removed.

## Data and interpretation

- Latency uses the existing v3 accumulator's eligible first receipts. The CDF is a fraction of latency samples, not network reachability. Images include the measurement definition and delivery windows.
- Message activity is the recorded publish/deliver/duplicate count divided by the bin duration. It includes local and late receipts and is not a delivery ratio.
- Control traffic counts send/recv/drop RPC observations per control type. One RPC can contain several types; send and receive are separate endpoint observations.
- Throughput covers all libp2p streams in kbit/s, excluding IP/TCP framing, retransmissions and management API traffic.
- Scores and degree use existing `observations.jsonl` records. Scores average observer-to-peer reports; degree counts unique neighbors in fresh mesh reports. Missing values are not filled with zero.

The browser generates PNGs from the existing `GET /api/v1/experiments/{id}/analysis` response. No Python runtime or image service is added. Whole-log metric calculation, observation collection and raw result ZIP formats remain in place. Requests do not repeat automatically.

[Experiment metrics](experiment-metrics.md) · [Monitoring and retention](monitoring.md)
