# Experiment result images

English | [Korean](visualization.kr.md)

Choose **Saved results → Images** to submit background analysis and track its progress. Graph images appear when analysis completes. Select an image or **PNG ↓** to download it. Each white-background PNG is 1,600 pixels wide and includes a title, run identity, snapshot time, axes and legend.

The image set contains first-delivery latency CDF, latency histogram and message activity. When the records exist, it also includes GossipSub control traffic, total P2P stream throughput, Peer scores by group and average GossipSub mesh degree by group. Groups appear as separate lines in the same image. Older results without topology/score history do not receive invented observations.

Accepted analysis continues after closing the dialog or browser. The Images button in **Saved results** displays queued, log-reading, calculating and ready states. The dialog polls every two seconds and shows bytes read and the current phase. Reading progress is relative to the total log size; aggregation and saving can remain after reading completes. **Retry** reconnects to an existing job after a status-request failure.

One analysis runs at a time, with at most 32 accepted jobs including queued work. Repeated clicks for the same result reuse its job. File lengths and snapshot time are captured when the worker starts. Use **Analyze latest snapshot** to include later records. Completed analysis is saved on the server, so reopening does not scan the logs again. **PNG ↓** downloads a chart rendered from that completed snapshot; **Download analysis JSON** downloads the saved data. PNG conversion happens in the browser when the completed result is opened.

Completed results survive Controller restart. Unfinished jobs become `interrupted`; **Retry** starts their analysis again from the beginning. Deleting a saved result interrupts its analysis and removes the stored output. When the Controller has an API token, submitting or retrying analysis requires it. Enter the token in the dialog when prompted, then choose **Retry**.

## Data and interpretation

- Latency uses the existing v3 accumulator's eligible first receipts. The CDF is a fraction of latency samples, not network reachability. Images include the measurement definition and delivery windows.
- Message activity is the recorded publish/deliver/duplicate count divided by the bin duration. It includes local and late receipts and is not a delivery ratio.
- Control traffic counts send/recv/drop RPC observations per control type. One RPC can contain several types; send and receive are separate endpoint observations.
- Throughput covers all libp2p streams in kbit/s, excluding IP/TCP framing, retransmissions and management API traffic.
- Scores and degree use existing `observations.jsonl` records. Scores average observer-to-peer reports; degree counts unique neighbors in fresh mesh reports. Missing values are not filled with zero.

Use `POST /api/v1/analysis-jobs/{id}` to submit, `GET` to inspect status, and `GET /api/v1/analysis-jobs/{id}/result?jobId={jobId}` to download completed analysis. Add `?refresh=1` to POST to replace a completed snapshot with a new analysis. The Dashboard uses these background endpoints without the previous two-minute analysis request deadline. The synchronous `/api/v1/experiments/{id}/analysis` remains available for compatibility with its two-minute limit. No Python runtime or image service is required. Raw result ZIP formats remain unchanged.

[Experiment metrics](experiment-metrics.md) · [Monitoring and retention](monitoring.md)
