const $ = (selector) => document.querySelector(selector);
const state = {
  snapshot: null, stream: null, reconnectTimer: null,
  savedResults: null, resultsLoading: false, resultsError: "",
  resultsRefreshTimer: null, resultsRefreshPending: false, runStates: null,
  savedScenarios: null, scenariosLoading: false, scenariosError: "", scenarioActionError: "",
  selectedScenarioId: null, scenarioLoadingId: null, scenarioSaving: false,
  scenarioDeletingId: null, pendingScenarioDeleteId: null, scenarioLoadVersion: 0, scenarioSubmitting: false,
  resultSizeInflight: new Set(), resultSizeQueue: [], resultSizeActive: 0, resultSizeUnavailable: new Set(),
  resultSizeExpiryTimer: null, resultSizeExpiryAt: 0,
  agentNumbers: loadAgentNumbers(),
  pendingStops: new Set(), deletedResultIDs: new Set(),
  pendingDelete: null, deletingResultId: null, apiToken: null,
  detailPanelObserver: null,
  topology: {
    layout: {},
    filters: { kademlia: true, gossipsub: true, transport: false, topic: "" },
    selected: null, hovered: null, graph: null,
    camera: { x: 0, y: 0, scale: 1 }, autoFit: true, drag: null,
    motion: { enabled: true, reduced: false, overridden: false, frame: null, lastFrame: 0 },
  },
};

const defaultScenario = `version: 1
name: gossip-smoke
seed: 42
phases:
  - name: boot nodes
    action: join
    group: boot
    role: boot
    count: 2
    interval:
      model: fixed
      value: 300ms
    node:
      topics: [kpl/default]

  - name: boot barrier
    action: wait-ready
    group: boot
    readyRatio: 1
    timeout: 45s

  - name: worker nodes
    action: join
    group: worker
    role: worker
    count: 12
    interval:
      model: exponential
      mean: 250ms
    lifetime:
      model: pareto
      xm: 45s
      alpha: 2.5
      max: 3m
    node:
      topics: [kpl/default]
      d: 6
      dLow: 4
      dHigh: 12
      dOut: 2
      dLazy: 6
      heartbeat: 1s

  - name: mesh barrier
    action: wait-ready
    group: worker
    readyRatio: 0.9
    timeout: 60s

  - name: publish sample
    action: publish
    group: worker
    count: 10
    payloadSize: 1024
    deliveryWindow: 10s
    interval:
      model: fixed
      value: 1s

  - name: settle
    action: wait
    duration: 15s

  - name: cleanup
    action: stop-all
`;

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>'"]/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[char]);
}

function loadAgentNumbers() {
  const numbers = new Map();
  const used = new Set();
  try {
    const saved = JSON.parse(localStorage.getItem("kpl-agent-numbers-v1") || "[]");
    if (!Array.isArray(saved)) return numbers;
    for (const entry of saved) {
      if (!Array.isArray(entry) || entry.length !== 2) continue;
      const [id, number] = entry;
      if (typeof id !== "string" || !id || !Number.isSafeInteger(number) || number < 1 || number >= 1000000 || numbers.has(id) || used.has(number)) continue;
      numbers.set(id, number);
      used.add(number);
    }
  } catch { /* Keep numbers stable in memory when browser storage is unavailable. */ }
  return numbers;
}

function rememberAgents(ids) {
  let next = 1;
  for (const number of state.agentNumbers.values()) next = Math.max(next, number + 1);
  let changed = false;
  for (const id of [...new Set(ids.filter((id) => typeof id === "string" && id))].sort()) {
    if (state.agentNumbers.has(id)) continue;
    state.agentNumbers.set(id, next++);
    changed = true;
  }
  if (changed) {
    try { localStorage.setItem("kpl-agent-numbers-v1", JSON.stringify([...state.agentNumbers])); }
    catch { /* The current page retains its mapping without persistent storage. */ }
  }
}

function agentNumber(id) {
  return state.agentNumbers.get(id) || "?";
}

function topologyData(nodes, edges) {
  const visibleNodes = nodes.filter((node) => node.state !== "stopping" && node.state !== "stopped");
  const ids = new Set(visibleNodes.map((node) => node.id));
  return { nodes: visibleNodes, edges: edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target)) };
}

function topologyProtocol(edge) {
  return edge.protocol === "kademlia" || edge.protocol === "gossipsub" ? edge.protocol : "transport";
}

function filterTopologyEdges(nodes, edges, filters) {
  const visible = topologyData(nodes, edges);
  const merged = new Map();
  for (const edge of visible.edges) {
    const protocol = topologyProtocol(edge);
    if (!filters[protocol] || edge.source === edge.target) continue;
    if (protocol === "gossipsub" && filters.topic && edge.topic !== filters.topic) continue;
    const [source, target] = [edge.source, edge.target].sort();
    const key = JSON.stringify([protocol, source, target]);
    if (!merged.has(key)) merged.set(key, { source, target, protocol, topics: new Set(), reportedBy: new Set(), topicReports: new Map() });
    const connection = merged.get(key);
    if (edge.topic) {
      connection.topics.add(edge.topic);
      if (!connection.topicReports.has(edge.topic)) connection.topicReports.set(edge.topic, new Set());
      for (const id of edge.reportedBy || []) connection.topicReports.get(edge.topic).add(id);
    }
    for (const id of edge.reportedBy || []) connection.reportedBy.add(id);
  }
  return [...merged.values()].map((edge) => ({ ...edge, topics: [...edge.topics].sort(), reportedBy: [...edge.reportedBy].sort(), topicReports: Object.fromEntries([...edge.topicReports].map(([topic, ids]) => [topic, [...ids].sort()])) }));
}

function topologyReportSummary(edge, selectedID) {
  const describe = (ids) => ids.includes(edge.source) && ids.includes(edge.target) ? "Both endpoints reported"
    : ids.includes(selectedID) ? "Selected Peer reported" : ids.length ? "Remote Peer reported" : "Reporter not available";
  if (edge.protocol === "gossipsub" && edge.topics.length) {
    return edge.topics.map((topic) => `${topic}: ${describe(edge.topicReports?.[topic] || [])}`).join("; ");
  }
  return describe(edge.reportedBy);
}

function observedTopologyTopics(nodes, edges) {
  const topics = new Set();
  for (const node of nodes) for (const topic of Object.keys(node.meshPeers || {})) topics.add(topic);
  for (const edge of edges) if (edge.protocol === "gossipsub" && edge.topic) topics.add(edge.topic);
  return [...topics].sort();
}

function fitTopologyCamera(bounds, width, height) {
  const scale = Math.min(1.8, Math.max(0.01, Math.min((width - 48) / bounds.width, (height - 48) / bounds.height)));
  return { x: (width - bounds.width * scale) / 2, y: (height - bounds.height * scale) / 2, scale };
}

function formatNumber(value, digits = 0) {
  return new Intl.NumberFormat("en-US", { maximumFractionDigits: digits }).format(Number(value || 0));
}

function formatBytes(value) {
  let size = value;
  if (!Number.isFinite(size) || size <= 0) return "";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit++;
  }
  const digits = unit === 0 || size >= 100 ? 0 : size >= 10 ? 1 : 2;
  return `${formatNumber(size, digits)} ${units[unit]}`;
}

function ratioRange(available, lower, upper, unknown) {
  if (!available) return "N/A";
  const percentage = (value) => `${formatNumber(value * 100, 1)}%`;
  return unknown > 0 ? `${percentage(lower)}–${percentage(upper)}` : percentage(lower);
}

function deliveryMetricView(metrics) {
  const windowed = metrics.definition === "session-window-v1";
  const n = (key) => formatNumber(metrics[key]);
  const coverageUpper = Number.isFinite(Number(metrics.stableCoverageUpperBound)) ? metrics.stableCoverageUpperBound : metrics.stableCoverage;
  return {
    windowed,
    label: windowed ? "Continuous-session delivery" : "Legacy dispatch delivery",
    primary: ratioRange(metrics.deliveryRatioAvailable, metrics.reachability, metrics.deliveryRatioUpperBound, windowed ? metrics.unknownDeliveries : 0),
    primaryDetail: windowed ? `On time: ${n("eligibleDeliveries")} / ${n("expectedDeliveries")} stable pairs` : `Reached: ${n("eligibleDeliveries")} / ${n("expectedDeliveries")} dispatch pairs`,
    progress: windowed ? `Windows: ${(metrics.deliveryWindows || []).join(", ") || "Awaiting publication"} · Mature: ${n("finalizedPublications")} · Pending: ${n("pendingPublications")}` : "Historical definition · No fixed deadline",
    initial: ratioRange(metrics.initialDeliveryRatioAvailable, metrics.initialDeliveryRatio, metrics.initialDeliveryRatioUpperBound, metrics.initialUnknownDeliveries),
    initialDetail: `On time: ${n("initialEligibleDeliveries")} / ${n("initialExpectedDeliveries")} known starting pairs`,
    coverage: ratioRange(metrics.stableCoverageAvailable, metrics.stableCoverage, coverageUpper, metrics.continuityUnknownPairs),
    coverageDetail: `Stable: ${n("expectedDeliveries")} / ${n("initialExpectedDeliveries")} known starting pairs · Departed: ${n("departedPairs")}`,
    observation: `${n("unknownDeliveries")} receipt unknown · ${n("continuityUnknownPairs")} continuity unknown · ${n("publicationAvailabilityUnknownPairs")} start unknown`,
    outcomes: `Known missed: ${n("missedDeliveries")} · Observed late: ${n("lateDeliveries")}`,
    note: windowed
      ? `Each publication uses its configured deliveryWindow. Only mature publications and sessions subscribed throughout that window enter the main ratio. Ranges are logical bounds from missing evidence, not confidence intervals. Starting delivery and coverage are conditional on the known starting cohort.${metrics.publicationAvailabilityUnknownPairs > 0 || metrics.measurementIncomplete ? " Some candidate starting sessions remain outside that cohort because their publication-time availability could not be proved; see observation quality." : ""}${metrics.legacyPublications > 0 || metrics.unscopedPublications > 0 ? " Publications without the required measurement evidence are excluded." : ""}`
      : "Historical dispatch cohorts have no fixed receipt deadline or session continuity evidence. New runs use continuous-session measurement.",
  };
}

function relativeTime(value) {
  const time = new Date(value).getTime();
  const seconds = Math.max(0, Math.round((Date.now() - time) / 1000));
  if (seconds < 5) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  return new Date(value).toLocaleString("en-US", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function token() {
  if (state.apiToken !== null) return state.apiToken;
  try { return localStorage.getItem("kpl-api-token") || ""; }
  catch { return ""; }
}

function saveToken(value) {
  state.apiToken = value.trim();
  try { localStorage.setItem("kpl-api-token", state.apiToken); }
  catch { /* Keep credentials for this page when browser storage is unavailable. */ }
}

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (token()) headers.set("Authorization", `Bearer ${token()}`);
  const response = await fetch(path, { ...options, headers });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    const failure = new Error(error.error || response.statusText);
    failure.status = response.status;
    throw failure;
  }
  if (response.status === 204) return null;
  return response.json();
}

const scenarioRequestTimeoutMs = 30000;

function scenarioTimeoutError(kind) {
  const message = kind === "run"
    ? "Request timed out. The experiment may have been submitted. Check Experiment progress before trying again."
    : kind === "mutation"
      ? "Request timed out. The server may have completed this operation. Refresh the saved scenario list before trying again."
      : "Request timed out.";
  const error = new Error(message);
  error.name = "TimeoutError";
  return error;
}

async function scenarioRequest(path, options = {}, kind = "read") {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), scenarioRequestTimeoutMs);
  try {
    return await api(path, { ...options, signal: controller.signal });
  } catch (error) {
    if (error.name === "AbortError") throw scenarioTimeoutError(kind);
    throw error;
  } finally {
    clearTimeout(timeout);
  }
}

function setConnection(mode, label) {
  const element = $("#connectionState");
  element.className = `connection-state ${mode}`;
  element.innerHTML = `<i></i>${escapeHTML(label)}`;
}

function connectStream() {
  if (state.stream) state.stream.close();
  const stream = new EventSource("/api/v1/stream");
  state.stream = stream;
  stream.addEventListener("snapshot", (event) => {
    state.snapshot = JSON.parse(event.data);
    render(state.snapshot);
    setConnection("live", "Live");
  });
  stream.onerror = () => {
    setConnection("offline", "Reconnecting");
    stream.close();
    clearTimeout(state.reconnectTimer);
    state.reconnectTimer = setTimeout(connectStream, 2000);
  };
}

function syncDetailPanelHeight() {
  const agents = $(".agents-panel");
  const events = $(".events-panel");
  if (!agents || !events) return;
  events.style.removeProperty("height");
  if (window.matchMedia?.("(max-width: 1050px)").matches) return;
  const height = agents.getBoundingClientRect().height;
  if (height > 0) events.style.height = `${height}px`;
}

function setupDetailPanelSizing() {
  syncDetailPanelHeight();
  if (typeof ResizeObserver === "undefined") return;
  state.detailPanelObserver?.disconnect();
  state.detailPanelObserver = new ResizeObserver(syncDetailPanelHeight);
  state.detailPanelObserver.observe($(".agents-panel"));
}

function render(snapshot) {
  const agents = snapshot.agents || [];
  const nodes = snapshot.nodes || [];
  const edges = snapshot.edges || [];
  const metrics = snapshot.metrics || {};
  const metricRun = (snapshot.experiments || []).find((run) => run.id === metrics.runId);
  const metricIteration = metricRun?.repetitions > 1 ? ` · Run ${formatNumber(metricRun.iteration)} of ${formatNumber(metricRun.repetitions)}` : "";
  $("#messageMetricsScope").textContent = metrics.runId
    ? `Message metrics: ${metricRun?.name ? `${metricRun.name} · ` : ""}${metrics.runId}${metricIteration}`
    : "Message metrics: No run selected";
  rememberAgents([...agents.map((agent) => agent.id), ...nodes.map((node) => node.agentId)]);
  const online = agents.filter((agent) => agent.state === "online").length;
  const capacity = agents.reduce((sum, agent) => sum + Math.max(0, agent.capacity - agent.activeNodes), 0);
  const ready = nodes.filter((node) => node.state === "ready").length;
  $("#agentMetric").textContent = `${online} / ${agents.length}`;
  $("#capacityMetric").textContent = `Available slots: ${formatNumber(capacity)}`;
  $("#peerMetric").textContent = formatNumber(ready);
  $("#connectionMetric").textContent = `Transport links: ${formatNumber(filterTopologyEdges(nodes, edges, { transport: true }).length)}`;
  $("#latencyMetric").textContent = metrics.latencySamples > 0 ? `${formatNumber(metrics.p95LatencyMs, 1)} ms` : "N/A";
  $("#averageLatencyMetric").textContent = metrics.latencySamples > 0 ? `Average: ${formatNumber(metrics.averageLatencyMs, 1)} ms · Samples: ${formatNumber(metrics.latencySamples)}` : "No eligible latency samples";
  const delivery = deliveryMetricView(metrics);
  $("#reachLabel").textContent = delivery.label;
  $("#reachMetric").textContent = delivery.primary;
  $("#deliveryMetric").textContent = delivery.primaryDetail;
  $("#measurementProgress").textContent = delivery.progress;
  $("#measurementSummary").hidden = !delivery.windowed;
  $("#initialReachMetric").textContent = delivery.initial;
  $("#initialDeliveryMetric").textContent = delivery.initialDetail;
  $("#coverageMetric").textContent = delivery.coverage;
  $("#coverageDetail").textContent = delivery.coverageDetail;
  $("#observationMetric").textContent = delivery.observation;
  $("#outcomesMetric").textContent = delivery.outcomes;
  $("#measurementNote").textContent = delivery.note;
  $("#eventTotalsMetric").textContent = `Published: ${formatNumber(metrics.published)} · Delivered: ${formatNumber(metrics.delivered)}`;
  $("#duplicateMetric").textContent = metrics.duplicateSamples > 0 ? formatNumber(metrics.averageDuplicates, 2) : "N/A";
  $("#duplicateSamplesMetric").textContent = `Eligible duplicates: ${formatNumber(metrics.eligibleDuplicates)} · Delivered pairs: ${formatNumber(metrics.duplicateSamples)}`;
  $("#duplicateTotalMetric").textContent = `All duplicate events: ${formatNumber(metrics.duplicates)}`;
  $("#updatedAt").textContent = new Date(snapshot.generatedAt).toLocaleTimeString("en-US");
  renderRuns(snapshot.experiments || []);
  renderAgents(agents);
  renderEvents(snapshot.events || []);
  syncDetailPanelHeight();
  renderTopology(nodes, edges);
}

function renderRuns(runs) {
  const runStates = JSON.stringify(runs.map((run) => [run.id, run.state]).sort((a, b) => String(a[0]).localeCompare(String(b[0]))));
  if (state.runStates !== null && state.runStates !== runStates) {
    clearTimeout(state.resultsRefreshTimer);
    state.resultsRefreshTimer = setTimeout(refreshSavedResults, 250);
  }
  state.runStates = runStates;
  $("#runCount").textContent = runs.length;
  if (!runs.length) {
    $("#runList").innerHTML = '<div class="empty-copy">No experiments yet.</div>';
    return;
  }
  $("#runList").innerHTML = runs.map((run) => {
    const progress = run.totalPhases ? Math.round((run.phase / run.totalPhases) * 100) : 0;
    const stopping = state.pendingStops.has(run.batchId || run.id);
    const stop = isPendingRun(run) ? `<button class="stop-button" data-stop-run="${escapeHTML(run.id)}" type="button" title="Stop this run and cancel the remaining queued runs in its batch." ${stopping ? "disabled" : ""}>${stopping ? "Stopping…" : run.repetitions > 1 ? "Stop batch" : "Stop"}</button>` : "";
    const downloadSize = runDownloadSize(run);
    return `<article class="run-item">
      <div class="run-title"><strong title="${escapeHTML(run.name)}">${escapeHTML(run.name)}</strong><span class="status-pill ${escapeHTML(run.state)}">${escapeHTML(run.state)}</span></div>
      <div class="run-meta"><span>${escapeHTML(run.state === "queued" ? "Waiting to start" : run.phaseName || `seed ${run.seed}`)}</span>${stop}</div>
      ${run.repetitions > 1 ? `<div class="run-meta"><span>Run ${formatNumber(run.iteration)} of ${formatNumber(run.repetitions)}</span></div>` : ""}
      <div class="run-meta"><span>Jobs: ${formatNumber(run.activeJobs || 0)} active · ${formatNumber(run.completedJobs || 0)} completed · ${formatNumber(run.failedJobs || 0)} failed · ${formatNumber(run.canceledJobs || 0)} canceled</span></div>
      <div class="progress-track" aria-label="${progress}% complete"><i style="width:${Math.min(100, progress)}%"></i></div>
      ${run.error ? `<div class="run-meta"><span>${escapeHTML(run.error)}</span></div>` : ""}
      <div class="run-actions">${resultDownloadLink(run)}${downloadSize}</div>
    </article>`;
  }).join("");
}

function isPendingRun(run) {
  return run.active || run.state === "running" || run.state === "queued";
}

function resultLocked(run) {
  if (isPendingRun(run)) return true;
  return Boolean(run.batchId && [...(state.savedResults || []), ...(state.snapshot?.experiments || [])].some((other) => other.batchId === run.batchId && isPendingRun(other)));
}

function resultDownloadLink(run) {
  if (run.state === "unreadable") {
    return '<span class="download-unavailable" title="Saved metadata could not be read.">Unavailable</span>';
  }
  const active = isPendingRun(run);
  const label = active ? "Download snapshot" : "Download results";
  const path = `/api/v1/experiments/${encodeURIComponent(run.id)}/download`;
  const title = active ? "Download a ZIP snapshot of the scenario, metadata, and events recorded so far."
    : "Download the saved scenario, metadata, and events as a ZIP file.";
  return `<a class="download-link" href="${escapeHTML(path)}" download="${escapeHTML(`${run.id}.zip`)}" target="_blank" rel="noopener" title="${title}" aria-label="${escapeHTML(`${label}: ${run.name || run.id}`)}">${label}</a>`;
}

function resultDownloadSize(run) {
  if (run.state === "unreadable") return "";
  if (isPendingRun(run)) {
    return '<span class="download-size live" title="This run is active; the ZIP size is determined when the download snapshot is created.">Live ZIP · size determined at download</span>';
  }
  const size = hasFreshResultDownloadSize(run) ? formatBytes(run.downloadBytes) : "";
  if (size) return `<span class="download-size" title="Size measured at last refresh; later events may change it.">ZIP · ${escapeHTML(size)}</span>`;
  return state.resultSizeUnavailable.has(run.id)
    ? '<span class="download-size" title="Size was not available at the last refresh.">Size unavailable</span>'
    : '<span class="download-size" title="Reading the exact size from the download endpoint.">Calculating ZIP size…</span>';
}

function runDownloadSize(run) {
  if (isPendingRun(run)) return resultDownloadSize(run);
  const saved = (state.savedResults || []).find((result) => result.id === run.id);
  return saved ? resultDownloadSize(saved) : "";
}

function resultSizeExpiryFromMaxAge(value, now = Date.now(), allowString = false) {
  const candidate = allowString && typeof value === "string" && /^\d+$/.test(value.trim()) ? Number(value.trim()) : value;
  if (typeof candidate !== "number" || !Number.isSafeInteger(candidate) || candidate <= 0) return 0;
  const expiry = now + candidate;
  return Number.isSafeInteger(expiry) && expiry > now ? expiry : 0;
}

function materializeResultDownloadSizeExpiry(run, now = Date.now()) {
  if (!run || !Object.prototype.hasOwnProperty.call(run, "downloadSizeMaxAgeMs")) return true;
  const expiry = resultSizeExpiryFromMaxAge(run.downloadSizeMaxAgeMs, now);
  delete run.downloadSizeMaxAgeMs;
  if (!formatBytes(run.downloadBytes) || !expiry) {
    delete run.downloadBytes;
    delete run.downloadSizeExpiresAtMs;
    return false;
  }
  run.downloadSizeExpiresAtMs = expiry;
  return true;
}

function hasFreshResultDownloadSize(run, now = Date.now()) {
  if (!formatBytes(run?.downloadBytes)) return false;
  if (run.downloadSizeExpiresAtMs === undefined || run.downloadSizeExpiresAtMs === null) return true;
  return Number.isSafeInteger(run.downloadSizeExpiresAtMs) && run.downloadSizeExpiresAtMs > now;
}

function needsResultDownloadSize(run) {
  return Boolean(run?.id && run.state !== "unreadable" && !isPendingRun(run) && !hasFreshResultDownloadSize(run));
}

function parseResultDownloadSizeHeaders(headers, now = Date.now()) {
  const rawBytes = headers.get("Content-Length");
  if (typeof rawBytes !== "string" || !/^\d+$/.test(rawBytes.trim())) throw new Error("Download size response has no valid Content-Length.");
  const downloadBytes = Number(rawBytes);
  if (!Number.isSafeInteger(downloadBytes) || downloadBytes <= 0) throw new Error("Download size is outside the supported range.");
  const rawMaxAge = headers.get("X-KPL-Result-Size-Max-Age-Ms");
  if (rawMaxAge === null || rawMaxAge === undefined) return { downloadBytes };
  const expiry = resultSizeExpiryFromMaxAge(rawMaxAge, now, true);
  if (!expiry) throw new Error("Download size response has no valid positive max-age.");
  return { downloadBytes, downloadSizeExpiresAtMs: expiry };
}

async function fetchResultDownloadBytes(id) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 30000);
  try {
    const path = `/api/v1/experiments/${encodeURIComponent(id)}/download`;
    const response = await fetch(path, { method: "HEAD", cache: "no-store", signal: controller.signal });
    if (!response.ok) throw new Error(`Download size request failed with status ${response.status}.`);
    return parseResultDownloadSizeHeaders(response.headers);
  } finally {
    clearTimeout(timeout);
  }
}

function renderResultSizeViews() {
  renderSavedResults();
  if (state.snapshot) renderRuns(state.snapshot.experiments || []);
}

async function resolveResultDownloadSize(id, requestedRun) {
  try {
    const measured = await fetchResultDownloadBytes(id);
    const run = (state.savedResults || []).find((result) => result.id === id);
    if (run === requestedRun) {
      if (needsResultDownloadSize(run)) {
        run.downloadBytes = measured.downloadBytes;
        if (measured.downloadSizeExpiresAtMs) run.downloadSizeExpiresAtMs = measured.downloadSizeExpiresAtMs;
        else delete run.downloadSizeExpiresAtMs;
      }
      state.resultSizeUnavailable.delete(id);
    }
  } catch {
    const run = (state.savedResults || []).find((result) => result.id === id);
    if (run === requestedRun && needsResultDownloadSize(run)) state.resultSizeUnavailable.add(id);
  } finally {
    state.resultSizeActive--;
    state.resultSizeInflight.delete(id);
    const current = (state.savedResults || []).find((result) => result.id === id);
    if (current !== requestedRun) enqueueResultDownloadSize(current);
    renderResultSizeViews();
    pumpResultSizeQueue();
    scheduleResultSizeExpiry();
  }
}

function pumpResultSizeQueue() {
  while (state.resultSizeActive < 2 && state.resultSizeQueue.length) {
    const id = state.resultSizeQueue.shift();
    const run = (state.savedResults || []).find((result) => result.id === id);
    if (!needsResultDownloadSize(run)) {
      state.resultSizeInflight.delete(id);
      continue;
    }
    state.resultSizeActive++;
    void resolveResultDownloadSize(id, run);
  }
}

function enqueueResultDownloadSize(run) {
  if (!needsResultDownloadSize(run) || state.resultSizeInflight.has(run.id)) return;
  state.resultSizeUnavailable.delete(run.id);
  state.resultSizeInflight.add(run.id);
  state.resultSizeQueue.push(run.id);
}

function discardExpiredResultDownloadSize(run, now = Date.now()) {
  if (!formatBytes(run?.downloadBytes) || run.downloadSizeExpiresAtMs === undefined || run.downloadSizeExpiresAtMs === null) return false;
  if (Number.isSafeInteger(run.downloadSizeExpiresAtMs) && run.downloadSizeExpiresAtMs > now) return false;
  delete run.downloadBytes;
  delete run.downloadSizeExpiresAtMs;
  return true;
}

function expireResultDownloadSizes() {
  state.resultSizeExpiryTimer = null;
  state.resultSizeExpiryAt = 0;
  const now = Date.now();
  for (const run of state.savedResults || []) {
    if (run.state === "unreadable" || isPendingRun(run)) continue;
    if (!discardExpiredResultDownloadSize(run, now)) continue;
    state.resultSizeUnavailable.delete(run.id);
    enqueueResultDownloadSize(run);
  }
  renderResultSizeViews();
  pumpResultSizeQueue();
  scheduleResultSizeExpiry();
}

function scheduleResultSizeExpiry() {
  const now = Date.now();
  let earliest = 0;
  for (const run of state.savedResults || []) {
    if (run.state === "unreadable" || isPendingRun(run)) continue;
    if (!hasFreshResultDownloadSize(run, now)) continue;
    const expiry = run.downloadSizeExpiresAtMs;
    if (expiry && (!earliest || expiry < earliest)) earliest = expiry;
  }
  if (state.resultSizeExpiryTimer && state.resultSizeExpiryAt === earliest) return;
  if (state.resultSizeExpiryTimer) clearTimeout(state.resultSizeExpiryTimer);
  state.resultSizeExpiryTimer = null;
  state.resultSizeExpiryAt = 0;
  if (!earliest) return;
  state.resultSizeExpiryAt = earliest;
  const delay = Math.min(Math.max(1, earliest - now), 2147483647);
  state.resultSizeExpiryTimer = setTimeout(expireResultDownloadSizes, delay);
}

function queueResultDownloadSizes(results) {
  const currentIDs = new Set(results.map((run) => run.id));
  for (const id of state.resultSizeUnavailable) if (!currentIDs.has(id)) state.resultSizeUnavailable.delete(id);
  for (const run of results) {
    materializeResultDownloadSizeExpiry(run);
    discardExpiredResultDownloadSize(run);
    if (!needsResultDownloadSize(run)) {
      state.resultSizeUnavailable.delete(run.id);
      continue;
    }
    enqueueResultDownloadSize(run);
  }
  pumpResultSizeQueue();
  scheduleResultSizeExpiry();
}

function formatResultTime(value) {
  if (!value || String(value).startsWith("0001-")) return "—";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "—";
  return date.toLocaleString("en-US", { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function formatScenarioID(value) {
  return value.length > 10 ? `${value.slice(0, 10)}…` : value;
}

function validateSavedScenario(value, requireYAML = false) {
  if (!value || typeof value !== "object" || typeof value.id !== "string" || !value.id
      || typeof value.name !== "string" || (requireYAML && typeof value.yaml !== "string")) {
    throw new Error("Unexpected saved scenario response.");
  }
  return value;
}

function scenarioOperationBusy() {
  return state.scenariosLoading || state.scenarioSaving || state.scenarioSubmitting
    || Boolean(state.scenarioLoadingId) || Boolean(state.scenarioDeletingId);
}

function renderSavedScenarios() {
  const scenarios = state.savedScenarios || [];
  const status = $("#scenarioLibraryStatus");
  const busy = scenarioOperationBusy();
  $(".scenario-library").setAttribute("aria-busy", String(busy));
  $("#refreshScenarios").disabled = busy;
  $("#refreshScenarios").textContent = state.scenariosLoading ? "Refreshing…" : "Refresh";
  $("#newScenario").disabled = busy;
  $("#saveScenario").disabled = busy;
  $("#saveScenario").textContent = state.scenarioSaving ? "Saving…"
    : state.selectedScenarioId ? "Save changes" : "Save scenario";
  $("#saveScenarioCopy").hidden = !state.selectedScenarioId;
  $("#saveScenarioCopy").disabled = busy;
  $("#scenarioName").disabled = busy;
  $("#scenarioText").disabled = busy;
  $("#apiToken").disabled = busy;
  $("#runRepetitions").disabled = busy;
  $("#runScenario").disabled = busy;
  $("#runScenario").textContent = state.scenarioSubmitting ? "Submitting…" : "Run";
  $("#scenarioLibraryError").textContent = state.scenarioActionError;
  status.classList.toggle("error", Boolean(state.scenariosError));
  status.setAttribute("role", state.scenariosError ? "alert" : "status");
  status.textContent = state.scenariosLoading ? "Loading saved scenarios…"
    : state.scenariosError ? `Could not load saved scenarios: ${state.scenariosError}${scenarios.length ? " Showing the last loaded list." : ""}`
    : state.savedScenarios === null ? "Open the editor to load saved scenarios."
    : scenarios.length ? "" : "No saved scenarios yet.";
  status.hidden = !status.textContent;

  const selected = scenarios.find((item) => item.id === state.selectedScenarioId);
  $("#scenarioEditingStatus").textContent = state.selectedScenarioId
    ? `Editing saved scenario · ${selected?.name || state.selectedScenarioId}`
    : "New unsaved scenario";
  $("#scenarioLibraryList").innerHTML = scenarios.map((item) => {
    const name = item.name || item.id;
    const isSelected = item.id === state.selectedScenarioId;
    const confirmingDelete = item.id === state.pendingScenarioDeleteId;
    const isDeleting = item.id === state.scenarioDeletingId;
    const isLoading = item.id === state.scenarioLoadingId;
    const updated = formatResultTime(item.updatedAt);
    const controls = confirmingDelete
      ? `<button class="secondary-button" type="button" data-cancel-scenario-delete="${escapeHTML(item.id)}" ${busy ? "disabled" : ""}>Cancel</button><button class="danger-button confirm-delete-scenario" type="button" data-confirm-scenario-delete="${escapeHTML(item.id)}" aria-label="${escapeHTML(`Confirm deletion of saved scenario: ${name}`)}" ${busy ? "disabled" : ""}>${isDeleting ? "Deleting…" : "Confirm delete"}</button>`
      : `<button class="secondary-button load-scenario-button" type="button" data-load-scenario="${escapeHTML(item.id)}" aria-label="${escapeHTML(`Load saved scenario: ${name}`)}" ${busy ? "disabled" : ""}>${isLoading ? "Loading…" : "Load"}</button><button class="secondary-button delete-scenario-button" type="button" data-delete-scenario="${escapeHTML(item.id)}" aria-label="${escapeHTML(`Delete saved scenario: ${name}`)}" ${busy ? "disabled" : ""}>Delete</button>`;
    return `<li class="scenario-library-item${isSelected ? " selected" : ""}"${isSelected ? ' aria-current="true"' : ""}>
      <div class="scenario-item-summary"><strong title="${escapeHTML(name)}">${escapeHTML(name)}</strong><small class="scenario-item-meta"><span>Updated ${escapeHTML(updated)}</span><span class="scenario-item-id" title="${escapeHTML(`Scenario ID: ${item.id}`)}"><span aria-hidden="true">ID ${escapeHTML(formatScenarioID(item.id))}</span><span class="visually-hidden">Scenario ID: ${escapeHTML(item.id)}</span></span></small></div>
      <div class="scenario-item-actions">${controls}</div>
    </li>`;
  }).join("");
  for (const close of document.querySelectorAll("[data-scenario-close]")) close.disabled = busy;
}

async function refreshSavedScenarios() {
  if (scenarioOperationBusy()) return;
  saveToken($("#apiToken").value);
  state.scenariosLoading = true;
  state.scenariosError = "";
  state.scenarioActionError = "";
  renderSavedScenarios();
  try {
    const response = await scenarioRequest("/api/v1/scenarios", { cache: "no-store" });
    if (!Array.isArray(response)) throw new Error("Unexpected saved scenarios response.");
    const scenarios = response.map((item) => validateSavedScenario(item));
    state.savedScenarios = scenarios;
    if (state.pendingScenarioDeleteId && !scenarios.some((item) => item.id === state.pendingScenarioDeleteId)) {
      state.pendingScenarioDeleteId = null;
    }
    if (state.selectedScenarioId && !scenarios.some((item) => item.id === state.selectedScenarioId)) {
      state.selectedScenarioId = null;
      state.scenarioActionError = "The selected saved scenario no longer exists. The editor was retained as a new scenario.";
    }
  } catch (error) {
    state.scenariosError = error.message;
  } finally {
    state.scenariosLoading = false;
    renderSavedScenarios();
  }
}

async function loadSavedScenario(id) {
  if (scenarioOperationBusy() || !(state.savedScenarios || []).some((item) => item.id === id)) return;
  saveToken($("#apiToken").value);
  const version = ++state.scenarioLoadVersion;
  state.scenarioLoadingId = id;
  state.pendingScenarioDeleteId = null;
  state.scenarioActionError = "";
  renderSavedScenarios();
  try {
    const item = validateSavedScenario(await scenarioRequest(`/api/v1/scenarios/${encodeURIComponent(id)}`, { cache: "no-store" }), true);
    if (version !== state.scenarioLoadVersion) return;
    state.selectedScenarioId = item.id;
    $("#scenarioName").value = item.name;
    $("#scenarioText").value = item.yaml;
    $("#scenarioText").focus();
  } catch (error) {
    if (version === state.scenarioLoadVersion) state.scenarioActionError = `Could not load the saved scenario: ${error.message}`;
  } finally {
    if (version === state.scenarioLoadVersion) state.scenarioLoadingId = null;
    renderSavedScenarios();
  }
}

function startNewScenario() {
  if (scenarioOperationBusy()) return;
  ++state.scenarioLoadVersion;
  state.selectedScenarioId = null;
  state.pendingScenarioDeleteId = null;
  state.scenarioActionError = "";
  $("#scenarioName").value = "";
  const editor = $("#scenarioText");
  editor.value = defaultScenario;
  renderSavedScenarios();
  $("#scenarioName").focus();
}

function upsertSavedScenario(item) {
  const summary = { id: item.id, name: item.name, createdAt: item.createdAt, updatedAt: item.updatedAt };
  state.savedScenarios = [summary, ...(state.savedScenarios || []).filter((saved) => saved.id !== item.id)];
}

function focusScenarioListAction(attribute, id) {
  if (!id) return false;
  const target = [...document.querySelectorAll(`[${attribute}]`)].find((button) => button.getAttribute(attribute) === id);
  target?.focus();
  return Boolean(target);
}

async function saveEditedScenario(asNew = false) {
  if (scenarioOperationBusy()) return;
  const name = $("#scenarioName").value.trim();
  const yaml = $("#scenarioText").value;
  if (!name) {
    state.scenarioActionError = "Enter a name for the saved scenario.";
    renderSavedScenarios();
    $("#scenarioName").focus();
    return;
  }
  if (!yaml.trim()) {
    state.scenarioActionError = "Enter a YAML scenario to save.";
    renderSavedScenarios();
    $("#scenarioText").focus();
    return;
  }
  saveToken($("#apiToken").value);
  const updateID = !asNew && state.selectedScenarioId;
  state.scenarioSaving = true;
  state.pendingScenarioDeleteId = null;
  state.scenarioActionError = "";
  renderSavedScenarios();
  try {
    const path = updateID ? `/api/v1/scenarios/${encodeURIComponent(updateID)}` : "/api/v1/scenarios";
    const item = validateSavedScenario(await scenarioRequest(path, {
      method: updateID ? "PUT" : "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, yaml }),
    }, "mutation"), true);
    state.selectedScenarioId = item.id;
    $("#scenarioName").value = item.name;
    $("#scenarioText").value = item.yaml;
    upsertSavedScenario(item);
    showToast(`${updateID ? "Updated" : "Saved"} scenario: ${item.name}.`);
  } catch (error) {
    state.scenarioActionError = `${updateID ? "Could not update" : "Could not save"} the scenario: ${error.message}`;
  } finally {
    state.scenarioSaving = false;
    renderSavedScenarios();
  }
}

function requestScenarioDeletion(id) {
  if (scenarioOperationBusy() || !(state.savedScenarios || []).some((item) => item.id === id)) return;
  state.pendingScenarioDeleteId = id;
  state.scenarioActionError = "";
  renderSavedScenarios();
  focusScenarioListAction("data-confirm-scenario-delete", id);
}

function cancelScenarioDeletion(id) {
  if (state.scenarioDeletingId || state.pendingScenarioDeleteId !== id) return;
  state.pendingScenarioDeleteId = null;
  renderSavedScenarios();
  focusScenarioListAction("data-delete-scenario", id);
}

async function confirmScenarioDeletion(id) {
  if (scenarioOperationBusy() || state.pendingScenarioDeleteId !== id) return;
  const item = (state.savedScenarios || []).find((saved) => saved.id === id);
  if (!item) return;
  const index = state.savedScenarios.indexOf(item);
  const focusAfterDelete = state.savedScenarios[index + 1]?.id || state.savedScenarios[index - 1]?.id;
  let deleted = false;
  saveToken($("#apiToken").value);
  state.scenarioDeletingId = id;
  state.scenarioActionError = "";
  renderSavedScenarios();
  try {
    try {
      await scenarioRequest(`/api/v1/scenarios/${encodeURIComponent(id)}`, { method: "DELETE" }, "mutation");
    } catch (error) {
      if (error.status !== 404) throw error;
    }
    state.savedScenarios = (state.savedScenarios || []).filter((saved) => saved.id !== id);
    state.pendingScenarioDeleteId = null;
    if (state.selectedScenarioId === id) state.selectedScenarioId = null;
    deleted = true;
    showToast(`Deleted saved scenario: ${item.name || item.id}.`);
  } catch (error) {
    state.scenarioActionError = `Could not delete the saved scenario: ${error.message}`;
  } finally {
    state.scenarioDeletingId = null;
    renderSavedScenarios();
    if (deleted) {
      if (!focusScenarioListAction("data-delete-scenario", focusAfterDelete)) $("#newScenario").focus();
    } else {
      focusScenarioListAction("data-confirm-scenario-delete", id);
    }
  }
}

async function refreshSavedResults() {
  clearTimeout(state.resultsRefreshTimer);
  state.resultsRefreshTimer = null;
  if (state.resultsLoading) {
    state.resultsRefreshPending = true;
    return;
  }
  state.resultsLoading = true;
  state.resultsError = "";
  renderSavedResults();
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 30000);
  let refreshed = false;
  try {
    const results = await api("/api/v1/results", { cache: "no-store", signal: controller.signal });
    if (!Array.isArray(results)) throw new Error("Unexpected saved results response.");
    state.savedResults = results.filter((run) => !state.deletedResultIDs.has(run.id));
    refreshed = true;
  } catch (error) {
    state.resultsError = error.name === "AbortError" ? "Request timed out." : error.message;
  } finally {
    clearTimeout(timeout);
    state.resultsLoading = false;
    if (refreshed) queueResultDownloadSizes(state.savedResults || []);
    renderResultSizeViews();
    if (state.resultsRefreshPending) {
      state.resultsRefreshPending = false;
      refreshSavedResults();
    }
  }
}

function renderSavedResults() {
  const results = state.savedResults || [];
  const status = $("#savedResultsStatus");
  const refresh = $("#refreshResults");
  refresh.disabled = state.resultsLoading;
  refresh.textContent = state.resultsLoading ? "Refreshing…" : "Refresh";
  $("#savedResultsTable").setAttribute("aria-busy", String(state.resultsLoading));
  status.classList.toggle("error", Boolean(state.resultsError));
  status.setAttribute("role", state.resultsError ? "alert" : "status");
  status.textContent = state.resultsLoading ? "Loading saved results…"
    : state.resultsError ? `Could not load saved results: ${state.resultsError} Use Refresh to try again.${results.length ? " Showing the last loaded list." : ""}`
    : results.length ? "" : "No saved results yet.";
  status.hidden = !status.textContent;
  $("#savedResultsTable").hidden = results.length === 0;
  $("#savedResultsRows").innerHTML = results.map((run) => {
    const stateHint = run.state === "interrupted" ? "Saved by a previous Controller; this run was not resumed."
      : run.state === "unreadable" ? "Saved metadata could not be read." : run.state;
    return `<tr>
      <td class="result-name"><strong>${escapeHTML(run.name || run.id)}</strong><span class="result-id">${escapeHTML(run.id)}</span>${run.repetitions > 1 ? `<span class="result-id">Run ${formatNumber(run.iteration)} of ${formatNumber(run.repetitions)}</span>` : ""}</td>
      <td><span class="status-pill ${escapeHTML(run.state)}" title="${escapeHTML(stateHint)}">${escapeHTML(run.state)}</span></td>
      <td>${escapeHTML(formatResultTime(run.startedAt))}</td>
      <td>${escapeHTML(formatResultTime(run.finishedAt))}</td>
      <td><div class="result-actions">${resultDownloadLink(run)}${resultDownloadSize(run)}<button class="delete-result-button" type="button" data-delete-result="${escapeHTML(run.id)}" aria-label="${escapeHTML(`Delete saved result: ${run.name || run.id}`)}" title="${resultLocked(run) ? "Available after this run and its batch have stopped." : "Delete this run's saved result."}" ${resultLocked(run) || state.deletingResultId ? "disabled" : ""}>${state.deletingResultId === run.id ? "Deleting…" : "Delete"}</button></div></td>
    </tr>`;
  }).join("");
}

function requestResultDeletion(id) {
  if (state.deletingResultId) return;
  const run = (state.savedResults || []).find((result) => result.id === id);
  if (!run || resultLocked(run)) return;
  state.pendingDelete = run;
  $("#deleteResultName").textContent = run.name || run.id;
  $("#deleteResultID").textContent = run.id;
  $("#deleteApiToken").value = token();
  $("#deleteResultError").textContent = "";
  $("#confirmDeleteResult").disabled = false;
  $("#confirmDeleteResult").textContent = "Delete result";
  $("#cancelDeleteResult").disabled = false;
  $("#deleteResultDialog").showModal();
}

async function confirmResultDeletion() {
  const run = state.pendingDelete;
  if (!run || state.deletingResultId) return;
  const latest = (state.savedResults || []).find((result) => result.id === run.id) || run;
  if (resultLocked(latest)) {
    $("#deleteResultError").textContent = "This run or its batch is active. Stop it before deleting its saved result.";
    return;
  }
  state.deletingResultId = run.id;
  saveToken($("#deleteApiToken").value);
  $("#apiToken").value = token();
  $("#deleteResultError").textContent = "";
  $("#confirmDeleteResult").disabled = true;
  $("#confirmDeleteResult").textContent = "Deleting…";
  $("#cancelDeleteResult").disabled = true;
  renderSavedResults();
  try {
    try {
      await api(`/api/v1/results/${encodeURIComponent(run.id)}`, { method: "DELETE" });
    } catch (error) {
      if (error.status !== 404) throw error;
    }
    state.deletedResultIDs.add(run.id);
    state.savedResults = (state.savedResults || []).filter((result) => result.id !== run.id);
    state.pendingDelete = null;
    $("#deleteResultDialog").close();
    showToast(`Deleted saved result: ${run.name || run.id}.`);
    await refreshSavedResults();
  } catch (error) {
    $("#deleteResultError").textContent = error.status === 409
      ? "This result is active, belongs to an active batch, or is being downloaded. Wait for it to finish, then try again."
      : `Could not delete the saved result: ${error.message}`;
    await refreshSavedResults();
  } finally {
    state.deletingResultId = null;
    $("#confirmDeleteResult").disabled = false;
    $("#confirmDeleteResult").textContent = "Delete result";
    $("#cancelDeleteResult").disabled = false;
    renderSavedResults();
  }
}

function renderAgents(agents) {
  rememberAgents(agents.map((agent) => agent.id));
  if (!agents.length) {
    $("#agentRows").innerHTML = '<tr><td colspan="7" class="empty-cell">No Agents registered.</td></tr>';
    return;
  }
  $("#agentRows").innerHTML = [...agents].sort((a, b) => agentNumber(a.id) - agentNumber(b.id)).map((agent) => {
    const usage = agent.capacity ? Math.min(100, Math.round((agent.activeNodes / agent.capacity) * 100)) : 0;
    return `<tr>
      <td class="agent-number">${agentNumber(agent.id)}</td>
      <td><span class="agent-name">${escapeHTML(agent.name)}</span><span class="agent-id">${escapeHTML(agent.id)}</span></td>
      <td><span class="state-dot ${escapeHTML(agent.state)}">${escapeHTML(agent.state)}</span></td>
      <td>${escapeHTML(agent.hostname || "—")}</td>
      <td>${formatNumber(agent.activeNodes)} / ${formatNumber(agent.capacity)}</td>
      <td><div class="usage"><div class="usage-track"><i style="width:${usage}%"></i></div><span>${usage}%</span></div></td>
      <td>${escapeHTML(relativeTime(agent.lastSeen))}</td>
    </tr>`;
  }).join("");
}

function renderEvents(events) {
  const recent = events.slice(-40).reverse();
  if (!recent.length) {
    $("#eventList").innerHTML = '<li class="empty-copy">No events collected.</li>';
    return;
  }
  $("#eventList").innerHTML = recent.map((event) => {
    const latency = event.fields?.latencyAvailable === false ? "latency unavailable" : event.latencyMs > 0 ? `${formatNumber(event.latencyMs, 1)} ms` : "";
    const nodePrefix = event.runId ? `${event.runId}-` : "";
    const nodeLabel = nodePrefix && event.nodeId?.startsWith(nodePrefix) ? event.nodeId.slice(nodePrefix.length) : event.nodeId;
    const detail = [nodeLabel, event.remotePeerId ? `← ${event.remotePeerId.slice(0, 10)}` : "", latency].filter(Boolean).join(" · ");
    const runID = event.runId ? `<small class="event-run-id" title="${escapeHTML(`Experiment ID: ${event.runId}`)}">Experiment · ${escapeHTML(event.runId)}</small>` : "";
    return `<li class="event-item"><time datetime="${escapeHTML(event.timestamp)}">${new Date(event.timestamp).toLocaleTimeString("en-US", { hour12: false })}</time><span class="event-type" title="${escapeHTML(event.type)}">${escapeHTML(event.type)}</span><span class="event-summary" title="${escapeHTML(detail)}">${escapeHTML(detail)}</span>${runID}</li>`;
  }).join("");
}

function svgElement(tag, attributes = {}, text) {
  const element = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const [key, value] of Object.entries(attributes)) element.setAttribute(key, value);
  if (text !== undefined) element.textContent = text;
  return element;
}

function renderTopology(nodes, edges) {
  ({ nodes, edges } = topologyData(nodes, edges));
  const topology = state.topology;
  stopTopologyMotion();
  rememberAgents(nodes.map((node) => node.agentId));
  if (!nodes.some((node) => node.id === topology.selected)) topology.selected = null;
  if (!nodes.some((node) => node.id === topology.hovered)) topology.hovered = null;
  const topicSelect = $("#topologyTopic");
  const topics = observedTopologyTopics(nodes, edges);
  if (topology.filters.topic && !topics.includes(topology.filters.topic)) topics.push(topology.filters.topic);
  const topicOptions = ["", ...topics];
  if (JSON.stringify([...topicSelect.options].map((option) => option.value)) !== JSON.stringify(topicOptions)) {
    topicSelect.replaceChildren(...topicOptions.map((topic) => {
      const option = document.createElement("option");
      option.value = topic;
      option.textContent = topic || "All topics";
      return option;
    }));
  }
  topicSelect.value = topology.filters.topic;
  const connections = filterTopologyEdges(nodes, edges, topology.filters);
  const agents = [...(state.snapshot?.agents || [])].sort((a, b) => agentNumber(a.id) - agentNumber(b.id));
  const svg = $("#topology");
  const focusedNodeID = document.activeElement?.closest?.("[data-node-id]")?.dataset.nodeId;
  const width = svg.clientWidth || 1000, height = svg.clientHeight || 560;
  const layout = layoutTopology(nodes, agents, topology.layout, width);
  const signature = JSON.stringify([
    nodes.map((node) => [node.id, node.agentId]).sort(),
    connections.map((edge) => [edge.source, edge.target, edge.protocol, edge.topics]).sort(),
    layout.groups.map((group) => group.id), layout.bounds,
  ]);
  const changed = topology.signature !== signature;
  topology.signature = signature;
  const graph = { nodes, edges: connections, ...layout, width, height, peerElements: [], edgeElements: [] };
  topology.graph = graph;
  if (changed) {
    reheatTopologyLayout(graph);
    // Respect reduced motion without sacrificing a readable initial layout.
    // User-paused layouts retain their existing positions instead.
    if (topology.motion.reduced && !topology.motion.overridden) {
      for (let i = 0; i < 36; i++) stepTopologyLayout(graph, connections, { pinnedID: topology.selected });
    }
  }
  $("#topologyEmpty").hidden = nodes.length > 0;
  $("#topologyLinkCount").textContent = `${formatNumber(nodes.length)} Peers · ${formatNumber(connections.length)} visible links`;
  const observed = nodes.filter((node) => node.overlayObservedAt && !node.overlayObservedAt.startsWith("0001-")).length;
  $("#topologyReportStatus").textContent = nodes.length && observed < nodes.length
    ? `Overlay reports: ${observed}/${nodes.length} Peers. Waiting for reports from the remaining Peers; older Peers may report transport only.`
    : "Each slice is an Agent. Peers arrange around their visible connections; lines show relationships, not packet traffic.";
  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  const world = svgElement("g", { id: "topologyWorld" });
  svg.replaceChildren(world);
  for (const [index, group] of layout.groups.entries()) {
    const background = svgElement("g", { class: "topology-agent", style: `--sector-hue: ${200 + (index % 4) * 17}` });
    background.append(svgElement("path", { d: topologySectorPath(group), class: "topology-agent-background" }));
    const label = svgElement("g", { class: "topology-agent-heading", transform: `translate(${group.labelX} ${group.labelY})` });
    label.append(svgElement("rect", { x: -21, y: -18, width: 42, height: 32, rx: 11, class: "topology-agent-badge" }));
    label.append(svgElement("text", { x: 0, y: 5, "text-anchor": "middle", class: "topology-label", "aria-label": `Agent ${agentNumber(group.id)}` }, agentNumber(group.id)));
    label.append(svgElement("text", { x: 0, y: 31, "text-anchor": "middle", class: "topology-agent-count" }, `${group.count} Peers`));
    label.append(svgElement("title", {}, `Agent ${agentNumber(group.id)} · ${group.id}`));
    background.append(label);
    world.append(background);
  }
  // Paint low-emphasis transport/routing relations below the GossipSub mesh.
  const order = { transport: 0, kademlia: 1, gossipsub: 2 };
  for (const edge of [...connections].sort((a, b) => order[a.protocol] - order[b.protocol])) {
    const path = svgElement("path", {
      class: `topology-edge ${edge.protocol}`, "data-source": edge.source, "data-target": edge.target,
    });
    const layer = edge.protocol === "gossipsub" ? "GossipSub mesh" : edge.protocol === "kademlia" ? "Kademlia routing" : "Transport";
    const reports = edge.protocol === "gossipsub" && edge.topics.length
      ? edge.topics.map((topic) => `${topic}: ${(edge.topicReports[topic] || []).join(", ") || "reporter not available"}`).join("\n")
      : edge.reportedBy.join(", ") || "reporter not available";
    path.append(svgElement("title", {}, `${layer}: ${edge.source} — ${edge.target}\nReported by: ${reports}`));
    world.append(path);
    graph.edgeElements.push({ element: path, edge });
  }
  if (layout.groups.length) {
    const { cx, cy } = layout.groups[0];
    const hub = svgElement("g", { class: "topology-hub", transform: `translate(${cx} ${cy})` });
    hub.append(svgElement("circle", { r: 32 }));
    hub.append(svgElement("text", { "text-anchor": "middle", y: 2, class: "topology-hub-count" }, formatNumber(nodes.length)));
    hub.append(svgElement("text", { "text-anchor": "middle", y: 17, class: "topology-hub-label" }, "PEERS"));
    world.append(hub);
  }
  for (const node of nodes) {
    const point = layout.positions.get(node.id);
    const mode = node.state !== "ready" ? "issue" : node.role === "boot" ? "boot" : "worker";
    const peer = svgElement("g", {
      class: "topology-peer", "data-node-id": node.id, tabindex: 0, role: "button",
      "aria-label": `Peer ${point.slot}, Agent ${agentNumber(node.agentId)}: ${node.id}, ${node.state}`,
      "aria-pressed": String(topology.selected === node.id),
    });
    peer.append(svgElement("circle", { r: 17, class: "topology-hit-area" }));
    peer.append(svgElement("circle", { r: node.role === "boot" ? 8 : 6.5, class: `topology-node ${mode}` }));
    peer.append(svgElement("text", { x: 0, y: 20, "text-anchor": "middle", class: "topology-slot" }, point.slot));
    peer.append(svgElement("title", {}, `${node.id}\nAgent ${agentNumber(node.agentId)} · ${node.role} · ${node.state}\nSelect to inspect relationships and Peer details.`));
    world.append(peer);
    graph.peerElements.push({ element: peer, id: node.id });
  }
  paintTopologyPositions();
  if (topology.autoFit) topology.camera = fitTopologyCamera(layout.bounds, width, height);
  applyTopologyCamera();
  highlightTopology();
  if (focusedNodeID) {
    [...svg.querySelectorAll("[data-node-id]")].find((peer) => peer.dataset.nodeId === focusedNodeID)?.focus({ preventScroll: true });
  }
  updateTopologyMotionControl();
  startTopologyMotion();
}

function paintTopologyPositions() {
  const graph = state.topology.graph;
  if (!graph) return;
  for (const { element, id } of graph.peerElements) {
    const point = graph.positions.get(id);
    element.setAttribute("transform", `translate(${point.x} ${point.y})`);
  }
  for (const { element, edge } of graph.edgeElements) {
    const a = graph.positions.get(edge.source), b = graph.positions.get(edge.target);
    const dx = b.x - a.x, dy = b.y - a.y, distance = Math.max(1, Math.hypot(dx, dy));
    const bend = edge.protocol === "kademlia" ? -12 : edge.protocol === "gossipsub" ? 12 : 0;
    element.setAttribute("d", `M ${a.x} ${a.y} Q ${(a.x + b.x) / 2 - dy / distance * bend} ${(a.y + b.y) / 2 + dx / distance * bend} ${b.x} ${b.y}`);
  }
}

function stopTopologyMotion() {
  const motion = state.topology.motion;
  if (motion.frame !== null) cancelAnimationFrame(motion.frame);
  motion.frame = null;
}

function startTopologyMotion() {
  const topology = state.topology, motion = topology.motion;
  if (!motion.enabled || document.hidden || !topology.graph?.nodes.length || motion.frame !== null) return;
  motion.frame = requestAnimationFrame(animateTopology);
}

function animateTopology(timestamp) {
  const topology = state.topology, motion = topology.motion;
  motion.frame = null;
  if (!motion.enabled || document.hidden || !topology.graph) return;
  // Cap work at 30 frames/s and avoid a jump after a background-tab pause.
  if (topology.drag || timestamp - motion.lastFrame < 1000 / 30) { startTopologyMotion(); return; }
  motion.lastFrame = timestamp;
  const moving = stepTopologyLayout(topology.graph, topology.graph.edges, { pinnedID: topology.hovered || topology.selected });
  paintTopologyPositions();
  if (moving) startTopologyMotion();
}

function updateTopologyMotionControl() {
  const button = $("#topologyMotion"), motion = state.topology.motion;
  button.textContent = motion.enabled ? "Pause motion" : "Resume motion";
  button.setAttribute("aria-pressed", String(!motion.enabled));
}

function applyTopologyCamera() {
  const { x, y, scale } = state.topology.camera;
  $("#topologyWorld")?.setAttribute("transform", `translate(${x} ${y}) scale(${scale})`);
  $("#topologyZoomLevel").textContent = `${Math.round(scale * 100)}%`;
}

function fitTopology() {
  const graph = state.topology.graph;
  if (!graph) return;
  state.topology.autoFit = true;
  state.topology.camera = fitTopologyCamera(graph.bounds, graph.width, graph.height);
  applyTopologyCamera();
}

function zoomTopology(factor, point) {
  const topology = state.topology, graph = topology.graph;
  if (!graph) return;
  const anchor = point || { x: graph.width / 2, y: graph.height / 2 };
  const camera = topology.camera;
  const scale = Math.max(0.01, Math.min(8, camera.scale * factor));
  const ratio = scale / camera.scale;
  topology.camera = { x: anchor.x - (anchor.x - camera.x) * ratio, y: anchor.y - (anchor.y - camera.y) * ratio, scale };
  topology.autoFit = false;
  applyTopologyCamera();
}

function highlightTopology() {
  const topology = state.topology, graph = topology.graph;
  if (!graph) return;
  const focused = topology.hovered || topology.selected;
  const neighbors = new Set([focused]);
  for (const edge of graph.edges) {
    if (edge.source === focused) neighbors.add(edge.target);
    if (edge.target === focused) neighbors.add(edge.source);
  }
  for (const edge of document.querySelectorAll(".topology-edge")) {
    const connected = edge.dataset.source === focused || edge.dataset.target === focused;
    edge.classList.toggle("is-active", Boolean(focused && connected));
    edge.classList.toggle("is-muted", Boolean(focused && !connected));
  }
  for (const peer of document.querySelectorAll(".topology-peer")) {
    peer.classList.toggle("is-selected", peer.dataset.nodeId === focused);
    peer.classList.toggle("is-muted", Boolean(focused && !neighbors.has(peer.dataset.nodeId)));
    peer.setAttribute("aria-pressed", String(peer.dataset.nodeId === topology.selected));
  }
  $("#clearTopologySelection").disabled = !topology.selected;
  renderTopologyDetails(graph.nodes.find((node) => node.id === focused));
}

function renderTopologyDetails(node) {
  const details = $("#topologyDetails");
  if (!node) {
    details.innerHTML = '<p class="topology-detail-hint">Hover over a Peer to highlight its visible neighbors. Select it to keep the details open. Drag the background to pan; use + / − or Ctrl + scroll to zoom.</p>';
    return;
  }
  const graph = state.topology.graph;
  const edges = graph.edges.filter((edge) => edge.source === node.id || edge.target === node.id);
  const layers = { kademlia: "Kademlia routing", gossipsub: "GossipSub mesh", transport: "Transport" };
  const metadata = node.metadata || {};
  const scores = Object.values(node.peerScores || {}).filter(Number.isFinite);
  const scoreSummary = scores.length
    ? `${formatNumber(scores.length)} scores · Average: ${(scores.reduce((sum, score) => sum + score, 0) / scores.length).toFixed(2)}`
    : "0 scores · Average: N/A";
  const pubsub = metadata.pubsubEnabled === "false" ? "Off" : `${metadata.pubsubRouter || "pubsub"} / ${metadata.topicMode || "subscribe"}`;
  const dht = metadata.dhtEnabled === "false" ? "Off" : metadata.dhtMode || "server";
  const entries = [
    ["Node ID", node.id], ["Peer ID", node.peerId || "Not reported"],
    ["Placement", `Agent ${agentNumber(node.agentId)} · Peer ${graph.positions.get(node.id).slot} · ${node.group || node.role || "—"}`],
    ["State / profile", `${node.state} · ${node.type || node.role || "—"}${node.profile ? ` · ${node.profile}` : ""}`],
    ["Overlay observed", formatResultTime(node.overlayObservedAt)],
    ["Reported peers", `${(node.routingPeers || []).length} routing · ${Object.values(node.meshPeers || {}).reduce((sum, peers) => sum + (peers || []).length, 0)} mesh memberships · ${(node.connectedPeers || []).length} transport`],
    ["Peer scores", scoreSummary], ["PubSub router / topic mode", pubsub], ["DHT mode", dht],
    ["Topics", metadata.topics || "No topics reported"], ["Runtime", metadata.runtime || "Not reported"],
  ];
  if (metadata.containerId) entries.push(["Container ID", metadata.containerId]);
  let network = {};
  try { network = JSON.parse(metadata.network || "{}"); } catch { /* Legacy metadata can omit network settings. */ }
  if (!network || typeof network !== "object") network = {};
  const shaping = [network.delay && `delay ${network.delay}`, network.jitter && `jitter ${network.jitter}`, network.lossPercent != null && `loss ${network.lossPercent}%`, network.rateMbps > 0 && `${network.rateMbps} Mbps`].filter(Boolean).join(" · ");
  if (shaping) entries.push(["Configured network", shaping]);
  if (node.error) entries.push(["Error", node.error]);
  const relations = edges.map((edge) => {
    const other = graph.nodes.find((candidate) => candidate.id === (edge.source === node.id ? edge.target : edge.source));
    const slot = graph.positions.get(other.id).slot;
    const reports = topologyReportSummary(edge, node.id);
    return `<li><i class="link-key ${edge.protocol}" aria-hidden="true"></i><span><strong>${escapeHTML(layers[edge.protocol])}</strong> · Agent ${agentNumber(other.agentId)} / Peer ${slot}<small>${escapeHTML(other.id)} · ${escapeHTML(reports)}</small></span></li>`;
  }).join("");
  details.innerHTML = `<dl class="topology-detail-grid">${entries.map(([label, value]) => `<div><dt>${escapeHTML(label)}</dt><dd>${escapeHTML(value)}</dd></div>`).join("")}</dl><div class="topology-neighbors"><h3>Visible relationships (${edges.length})</h3>${relations ? `<ul>${relations}</ul>` : '<p class="topology-detail-hint">No relationships match the current layer and topic filters.</p>'}</div>`;
}

function setupTopologyControls() {
  const motion = state.topology.motion;
  const preference = window.matchMedia?.("(prefers-reduced-motion: reduce)");
  const applyPreference = () => {
    motion.reduced = Boolean(preference?.matches);
    if (!motion.overridden) motion.enabled = !motion.reduced;
    updateTopologyMotionControl();
    if (!motion.enabled) stopTopologyMotion();
    else startTopologyMotion();
  };
  applyPreference();
  preference?.addEventListener("change", applyPreference);
  $("#topologyMotion").addEventListener("click", () => {
    motion.overridden = true;
    motion.enabled = !motion.enabled;
    updateTopologyMotionControl();
    if (!motion.enabled) stopTopologyMotion();
    else {
      if (state.topology.graph) reheatTopologyLayout(state.topology.graph, 0.3);
      startTopologyMotion();
    }
  });
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) stopTopologyMotion();
    else startTopologyMotion();
  });
  window.addEventListener("pagehide", stopTopologyMotion);
  for (const protocol of ["kademlia", "gossipsub", "transport"]) {
    $(`#show${protocol}`).addEventListener("change", (event) => {
      state.topology.filters[protocol] = event.target.checked;
      if (state.snapshot) renderTopology(state.snapshot.nodes || [], state.snapshot.edges || []);
    });
  }
  $("#topologyTopic").addEventListener("change", (event) => {
    state.topology.filters.topic = event.target.value;
    if (state.snapshot) renderTopology(state.snapshot.nodes || [], state.snapshot.edges || []);
  });
  $("#topologyZoomIn").addEventListener("click", () => zoomTopology(1.25));
  $("#topologyZoomOut").addEventListener("click", () => zoomTopology(0.8));
  $("#topologyFit").addEventListener("click", fitTopology);
  $("#clearTopologySelection").addEventListener("click", () => { state.topology.selected = null; state.topology.hovered = null; highlightTopology(); });
  const svg = $("#topology");
  const focusPeer = (event) => event.target.closest("[data-node-id]")?.dataset.nodeId || null;
  svg.addEventListener("pointerover", (event) => { state.topology.hovered = focusPeer(event); highlightTopology(); });
  svg.addEventListener("pointerleave", () => { state.topology.hovered = null; highlightTopology(); });
  svg.addEventListener("focusin", (event) => { state.topology.hovered = focusPeer(event); highlightTopology(); });
  svg.addEventListener("focusout", () => { state.topology.hovered = null; highlightTopology(); });
  const selectPeer = (id) => { state.topology.selected = state.topology.selected === id ? null : id; highlightTopology(); };
  svg.addEventListener("click", (event) => {
    if (state.topology.suppressClick) { state.topology.suppressClick = false; return; }
    const id = focusPeer(event);
    if (id) selectPeer(id);
  });
  svg.addEventListener("keydown", (event) => {
    const id = focusPeer(event);
    if (id && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); selectPeer(id); }
    if (event.key === "Escape") { state.topology.selected = null; state.topology.hovered = null; highlightTopology(); }
  });
  svg.addEventListener("pointerdown", (event) => {
    state.topology.suppressClick = false;
    if (event.button !== 0 || focusPeer(event)) return;
    state.topology.drag = { id: event.pointerId, x: event.clientX, y: event.clientY, camera: { ...state.topology.camera } };
    svg.setPointerCapture(event.pointerId);
    svg.classList.add("is-panning");
  });
  svg.addEventListener("pointermove", (event) => {
    const drag = state.topology.drag;
    if (!drag || drag.id !== event.pointerId) return;
    const dx = event.clientX - drag.x, dy = event.clientY - drag.y;
    if (Math.hypot(dx, dy) > 3) state.topology.suppressClick = true;
    state.topology.camera = { ...drag.camera, x: drag.camera.x + dx, y: drag.camera.y + dy };
    state.topology.autoFit = false;
    applyTopologyCamera();
  });
  const finishPan = () => { state.topology.drag = null; svg.classList.remove("is-panning"); };
  svg.addEventListener("pointerup", finishPan);
  svg.addEventListener("pointercancel", finishPan);
  svg.addEventListener("lostpointercapture", finishPan);
  svg.addEventListener("wheel", (event) => {
    if (!event.ctrlKey && !event.metaKey) return;
    event.preventDefault();
    const rect = svg.getBoundingClientRect();
    zoomTopology(Math.exp(-event.deltaY * 0.005), { x: event.clientX - rect.left, y: event.clientY - rect.top });
  }, { passive: false });
}

async function submitScenarioRun() {
  if (scenarioOperationBusy()) return;
  const error = $("#scenarioError");
  const repetitions = Number($("#runRepetitions").value);
  if (!Number.isInteger(repetitions) || repetitions < 1 || repetitions > 100) {
    error.textContent = "Enter a whole number of runs from 1 to 100.";
    $("#runRepetitions").focus();
    return;
  }
  saveToken($("#apiToken").value);
  state.scenarioSubmitting = true;
  state.pendingScenarioDeleteId = null;
  state.scenarioActionError = "";
  error.textContent = "";
  renderSavedScenarios();
  try {
    const run = await scenarioRequest("/api/v1/experiments", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ scenario: $("#scenarioText").value, repetitions }),
    }, "run");
    $("#scenarioDialog").close();
    showToast(repetitions > 1 ? `Queued ${repetitions} runs: ${run.name}.` : `Submitted experiment: ${run.name}.`);
  } catch (caught) {
    error.textContent = caught.message;
  } finally {
    state.scenarioSubmitting = false;
    renderSavedScenarios();
  }
}

function closeScenarioEditor() {
  if (!scenarioOperationBusy()) $("#scenarioDialog").close("cancel");
}

function handleScenarioNameKeydown(event) {
  if (event.key !== "Enter" || event.isComposing) return;
  event.preventDefault();
  if (!scenarioOperationBusy()) saveEditedScenario(false);
}

function showToast(message) {
  const toast = $("#toast");
  toast.textContent = message;
  toast.classList.add("show");
  setTimeout(() => toast.classList.remove("show"), 2600);
}

$("#scenarioText").value = defaultScenario;
$("#apiToken").value = token();
$("#refreshResults").addEventListener("click", refreshSavedResults);
$("#openScenario").addEventListener("click", () => {
  $("#scenarioDialog").showModal();
  renderSavedScenarios();
  refreshSavedScenarios();
});
$("#refreshScenarios").addEventListener("click", refreshSavedScenarios);
$("#newScenario").addEventListener("click", startNewScenario);
$("#saveScenario").addEventListener("click", () => saveEditedScenario(false));
$("#saveScenarioCopy").addEventListener("click", () => saveEditedScenario(true));
$("#runScenario").addEventListener("click", submitScenarioRun);
$("#scenarioName").addEventListener("keydown", handleScenarioNameKeydown);
$("#scenarioForm").addEventListener("submit", (event) => event.preventDefault());
for (const close of document.querySelectorAll("[data-scenario-close]")) close.addEventListener("click", closeScenarioEditor);

document.addEventListener("click", async (event) => {
  const loadScenarioButton = event.target.closest("[data-load-scenario]");
  if (loadScenarioButton) {
    if (!loadScenarioButton.disabled) loadSavedScenario(loadScenarioButton.dataset.loadScenario);
    return;
  }
  const deleteScenarioButton = event.target.closest("[data-delete-scenario]");
  if (deleteScenarioButton) {
    if (!deleteScenarioButton.disabled) requestScenarioDeletion(deleteScenarioButton.dataset.deleteScenario);
    return;
  }
  const cancelScenarioDeleteButton = event.target.closest("[data-cancel-scenario-delete]");
  if (cancelScenarioDeleteButton) {
    if (!cancelScenarioDeleteButton.disabled) cancelScenarioDeletion(cancelScenarioDeleteButton.dataset.cancelScenarioDelete);
    return;
  }
  const confirmScenarioDeleteButton = event.target.closest("[data-confirm-scenario-delete]");
  if (confirmScenarioDeleteButton) {
    if (!confirmScenarioDeleteButton.disabled) confirmScenarioDeletion(confirmScenarioDeleteButton.dataset.confirmScenarioDelete);
    return;
  }
  const deleteButton = event.target.closest("[data-delete-result]");
  if (deleteButton) {
    if (!deleteButton.disabled) requestResultDeletion(deleteButton.dataset.deleteResult);
    return;
  }
  const button = event.target.closest("[data-stop-run]");
  if (!button || button.disabled) return;
  const run = (state.snapshot?.experiments || []).find((item) => item.id === button.dataset.stopRun);
  const key = run?.batchId || button.dataset.stopRun;
  if (state.pendingStops.has(key)) return;
  state.pendingStops.add(key);
  button.disabled = true;
  if (state.snapshot) renderRuns(state.snapshot.experiments || []);
  try {
    await api(`/api/v1/experiments/${encodeURIComponent(button.dataset.stopRun)}/stop`, { method: "POST" });
    showToast("Stop requested. Remaining queued runs in this batch will be canceled.");
  } catch (caught) {
    showToast(caught.message);
  } finally {
    state.pendingStops.delete(key);
    button.disabled = false;
    if (state.snapshot) renderRuns(state.snapshot.experiments || []);
  }
});

$("#confirmDeleteResult").addEventListener("click", confirmResultDeletion);
$("#scenarioDialog").addEventListener("cancel", (event) => {
  if (scenarioOperationBusy()) event.preventDefault();
});
$("#scenarioDialog").addEventListener("close", () => {
  if (!state.scenarioDeletingId) state.pendingScenarioDeleteId = null;
});
$("#deleteResultDialog").addEventListener("cancel", (event) => {
  if (state.deletingResultId) event.preventDefault();
});
$("#deleteResultDialog").addEventListener("close", () => {
  if (!state.deletingResultId) state.pendingDelete = null;
});

setupTopologyControls();
setupDetailPanelSizing();
renderSavedScenarios();
window.addEventListener("resize", () => {
  syncDetailPanelHeight();
  if (state.snapshot) renderTopology(state.snapshot.nodes || [], state.snapshot.edges || []);
});
refreshSavedResults();
connectStream();
