const $ = (selector) => document.querySelector(selector);
const state = {
  snapshot: null, stream: null, reconnectTimer: null,
  savedResults: null, resultsLoading: false, resultsError: "",
  resultsRefreshTimer: null, resultsRefreshPending: false, runStates: null,
  agentNumbers: loadAgentNumbers(),
  pendingStops: new Set(), deletedResultIDs: new Set(),
  pendingDelete: null, deletingResultId: null, apiToken: null,
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
    interval:
      model: fixed
      value: 1s

  - name: settle
    action: wait
    duration: 5s

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

function formatNumber(value, digits = 0) {
  return new Intl.NumberFormat("en-US", { maximumFractionDigits: digits }).format(Number(value || 0));
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
  $("#connectionMetric").textContent = `Connections: ${formatNumber(topologyData(nodes, edges).edges.length)}`;
  $("#latencyMetric").textContent = metrics.latencySamples > 0 ? `${formatNumber(metrics.p95LatencyMs, 1)} ms` : "N/A";
  $("#averageLatencyMetric").textContent = metrics.latencySamples > 0 ? `Average: ${formatNumber(metrics.averageLatencyMs, 1)} ms · Samples: ${formatNumber(metrics.latencySamples)}` : "No eligible latency samples";
  $("#reachMetric").textContent = metrics.deliveryRatioAvailable ? `${formatNumber(metrics.reachability * 100, 1)}%` : "N/A";
  $("#deliveryMetric").textContent = `Eligible deliveries: ${formatNumber(metrics.eligibleDeliveries)} / ${formatNumber(metrics.expectedDeliveries)}`;
  $("#eventTotalsMetric").textContent = `Published: ${formatNumber(metrics.published)} · Delivered: ${formatNumber(metrics.delivered)}`;
  $("#duplicateMetric").textContent = metrics.duplicateSamples > 0 ? formatNumber(metrics.averageDuplicates, 2) : "N/A";
  $("#duplicateSamplesMetric").textContent = `Eligible duplicates: ${formatNumber(metrics.eligibleDuplicates)} · Delivered pairs: ${formatNumber(metrics.duplicateSamples)}`;
  $("#duplicateTotalMetric").textContent = `All duplicate events: ${formatNumber(metrics.duplicates)}`;
  $("#updatedAt").textContent = new Date(snapshot.generatedAt).toLocaleTimeString("en-US");
  renderRuns(snapshot.experiments || []);
  renderAgents(agents);
  renderEvents(snapshot.events || []);
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
    return `<article class="run-item">
      <div class="run-title"><strong title="${escapeHTML(run.name)}">${escapeHTML(run.name)}</strong><span class="status-pill ${escapeHTML(run.state)}">${escapeHTML(run.state)}</span></div>
      <div class="run-meta"><span>${escapeHTML(run.state === "queued" ? "Waiting to start" : run.phaseName || `seed ${run.seed}`)}</span>${stop}</div>
      ${run.repetitions > 1 ? `<div class="run-meta"><span>Run ${formatNumber(run.iteration)} of ${formatNumber(run.repetitions)}</span></div>` : ""}
      <div class="run-meta"><span>Jobs: ${formatNumber(run.activeJobs || 0)} active · ${formatNumber(run.completedJobs || 0)} completed · ${formatNumber(run.failedJobs || 0)} failed · ${formatNumber(run.canceledJobs || 0)} canceled</span></div>
      <div class="progress-track" aria-label="${progress}% complete"><i style="width:${Math.min(100, progress)}%"></i></div>
      ${run.error ? `<div class="run-meta"><span>${escapeHTML(run.error)}</span></div>` : ""}
      <div class="run-actions">${resultDownloadLink(run)}</div>
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

function formatResultTime(value) {
  if (!value || String(value).startsWith("0001-")) return "—";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "—";
  return date.toLocaleString("en-US", { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
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
  try {
    const results = await api("/api/v1/results", { cache: "no-store", signal: controller.signal });
    if (!Array.isArray(results)) throw new Error("Unexpected saved results response.");
    state.savedResults = results.filter((run) => !state.deletedResultIDs.has(run.id));
  } catch (error) {
    state.resultsError = error.name === "AbortError" ? "Request timed out." : error.message;
  } finally {
    clearTimeout(timeout);
    state.resultsLoading = false;
    renderSavedResults();
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
      <td><div class="result-actions">${resultDownloadLink(run)}<button class="delete-result-button" type="button" data-delete-result="${escapeHTML(run.id)}" aria-label="${escapeHTML(`Delete saved result: ${run.name || run.id}`)}" title="${resultLocked(run) ? "Available after this run and its batch have stopped." : "Delete this run's saved result."}" ${resultLocked(run) || state.deletingResultId ? "disabled" : ""}>${state.deletingResultId === run.id ? "Deleting…" : "Delete"}</button></div></td>
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
    const detail = [event.nodeId, event.remotePeerId ? `← ${event.remotePeerId.slice(0, 10)}` : "", latency].filter(Boolean).join(" · ");
    return `<li class="event-item"><time>${new Date(event.timestamp).toLocaleTimeString("en-US", { hour12: false })}</time><span class="event-type">${escapeHTML(event.type)}</span><span class="event-summary" title="${escapeHTML(detail)}">${escapeHTML(detail)}</span></li>`;
  }).join("");
}

function renderTopology(nodes, edges) {
  ({ nodes, edges } = topologyData(nodes, edges));
  rememberAgents(nodes.map((node) => node.agentId));
  const svg = $("#topology");
  svg.replaceChildren();
  $("#topologyEmpty").hidden = nodes.length > 0;
  if (!nodes.length) return;
  const width = Math.max(760, svg.clientWidth || 760);
  const height = Math.max(368, svg.clientHeight || 368);
  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  const agents = [...new Set(nodes.map((node) => node.agentId))].sort((a, b) => agentNumber(a) - agentNumber(b));
  const positions = new Map();
  agents.forEach((agentId, agentIndex) => {
    const group = nodes.filter((node) => node.agentId === agentId).sort((a, b) => String(a.id).localeCompare(String(b.id)));
    const centerX = ((agentIndex + 1) / (agents.length + 1)) * width;
    const centerY = height / 2;
    const radius = Math.min(width / Math.max(agents.length * 2.4, 3), height * 0.34);
    group.forEach((node, index) => {
      const angle = (Math.PI * 2 * index) / Math.max(group.length, 1) - Math.PI / 2;
      const layer = group.length === 1 ? 0 : radius * (0.62 + (index % 3) * 0.16);
      positions.set(node.id, { x: centerX + Math.cos(angle) * layer, y: centerY + Math.sin(angle) * layer });
    });
    const label = document.createElementNS("http://www.w3.org/2000/svg", "text");
    label.setAttribute("x", centerX);
    label.setAttribute("y", 24);
    label.setAttribute("text-anchor", "middle");
    label.setAttribute("class", "topology-label");
    label.textContent = agentNumber(agentId);
    label.setAttribute("aria-label", `Agent ${agentNumber(agentId)}`);
    svg.append(label);
  });
  edges.forEach((edge) => {
    const source = positions.get(edge.source), target = positions.get(edge.target);
    if (!source || !target) return;
    const line = document.createElementNS("http://www.w3.org/2000/svg", "line");
    line.setAttribute("x1", source.x); line.setAttribute("y1", source.y);
    line.setAttribute("x2", target.x); line.setAttribute("y2", target.y);
    line.setAttribute("class", "topology-edge");
    svg.append(line);
  });
  nodes.forEach((node) => {
    const point = positions.get(node.id);
    const circle = document.createElementNS("http://www.w3.org/2000/svg", "circle");
    const mode = node.state !== "ready" ? "issue" : node.role === "boot" ? "boot" : "worker";
    circle.setAttribute("cx", point.x); circle.setAttribute("cy", point.y);
    circle.setAttribute("r", node.role === "boot" ? 7 : 5);
    circle.setAttribute("class", `topology-node ${mode}`);
    const title = document.createElementNS("http://www.w3.org/2000/svg", "title");
    const scoreValues = Object.values(node.peerScores || {});
    const scoreText = scoreValues.length ? ` · ${scoreValues.length} scores (avg ${(scoreValues.reduce((sum, score) => sum + score, 0) / scoreValues.length).toFixed(2)})` : "";
    const metadata = node.metadata || {};
    const pubsubText = metadata.pubsubEnabled === "false" ? "off" : `${metadata.pubsubRouter || "pubsub"}/${metadata.topicMode || "subscribe"}`;
    const dhtText = metadata.dhtEnabled === "false" ? "off" : metadata.dhtMode || "server";
    let network = {};
    try { network = JSON.parse(metadata.network || "{}"); } catch { /* older Agents may omit network metadata */ }
    const networkText = [network.delay && `delay ${network.delay}`, network.jitter && `jitter ${network.jitter}`, network.lossPercent != null && `loss ${network.lossPercent}%`, network.rateMbps > 0 && `${network.rateMbps} Mbps`].filter(Boolean).join(" · ");
    const runtimeText = metadata.runtime ? `\nRuntime ${metadata.runtime}${metadata.containerId ? ` · ${metadata.containerId.slice(0, 12)}` : ""}` : "";
    title.textContent = `${node.id}\nAgent ${agentNumber(node.agentId)} · ${node.type || node.role} · ${node.state} · ${node.connectedPeers?.length || 0} connections${scoreText}\nPubSub ${pubsubText} · DHT ${dhtText} · ${metadata.topics || "no topics"}${runtimeText}${networkText ? `\n${networkText}` : ""}`;
    circle.append(title);
    svg.append(circle);
  });
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
$("#openScenario").addEventListener("click", () => $("#scenarioDialog").showModal());
$("#runScenario").addEventListener("click", async () => {
  const button = $("#runScenario");
  const error = $("#scenarioError");
  if (button.disabled) return;
  const repetitions = Number($("#runRepetitions").value);
  if (!Number.isInteger(repetitions) || repetitions < 1 || repetitions > 100) {
    error.textContent = "Enter a whole number of runs from 1 to 100.";
    return;
  }
  saveToken($("#apiToken").value);
  button.disabled = true;
  $("#runRepetitions").disabled = true;
  error.textContent = "";
  try {
    const run = await api("/api/v1/experiments", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ scenario: $("#scenarioText").value, repetitions }) });
    $("#scenarioDialog").close();
    showToast(repetitions > 1 ? `Queued ${repetitions} runs: ${run.name}.` : `Submitted experiment: ${run.name}.`);
  } catch (caught) {
    error.textContent = caught.message;
  } finally {
    button.disabled = false;
    $("#runRepetitions").disabled = false;
  }
});

document.addEventListener("click", async (event) => {
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
$("#deleteResultDialog").addEventListener("cancel", (event) => {
  if (state.deletingResultId) event.preventDefault();
});
$("#deleteResultDialog").addEventListener("close", () => {
  if (!state.deletingResultId) state.pendingDelete = null;
});

window.addEventListener("resize", () => state.snapshot && renderTopology(state.snapshot.nodes || [], state.snapshot.edges || []));
refreshSavedResults();
connectStream();
