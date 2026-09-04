const $ = (selector) => document.querySelector(selector);
const state = { snapshot: null, stream: null, reconnectTimer: null };

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

function formatNumber(value, digits = 0) {
  return new Intl.NumberFormat("ko-KR", { maximumFractionDigits: digits }).format(Number(value || 0));
}

function relativeTime(value) {
  const time = new Date(value).getTime();
  const seconds = Math.max(0, Math.round((Date.now() - time) / 1000));
  if (seconds < 5) return "방금 전";
  if (seconds < 60) return `${seconds}초 전`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}분 전`;
  return new Date(value).toLocaleString("ko-KR", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function token() {
  return localStorage.getItem("kpl-api-token") || "";
}

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (token()) headers.set("Authorization", `Bearer ${token()}`);
  const response = await fetch(path, { ...options, headers });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error || response.statusText);
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
    setConnection("live", "실시간 연결");
  });
  stream.onerror = () => {
    setConnection("offline", "재연결 중");
    stream.close();
    clearTimeout(state.reconnectTimer);
    state.reconnectTimer = setTimeout(connectStream, 2000);
  };
}

function render(snapshot) {
  const agents = snapshot.agents || [];
  const nodes = snapshot.nodes || [];
  const edges = snapshot.edges || [];
  const online = agents.filter((agent) => agent.state === "online").length;
  const capacity = agents.reduce((sum, agent) => sum + Math.max(0, agent.capacity - agent.activeNodes), 0);
  const ready = nodes.filter((node) => node.state === "ready").length;
  $("#agentMetric").textContent = `${online} / ${agents.length}`;
  $("#capacityMetric").textContent = `가용 슬롯 ${formatNumber(capacity)}`;
  $("#peerMetric").textContent = formatNumber(ready);
  $("#connectionMetric").textContent = `연결 ${formatNumber(edges.length)}개`;
  $("#latencyMetric").textContent = snapshot.metrics.p95LatencyMs ? `${formatNumber(snapshot.metrics.p95LatencyMs, 1)} ms` : "—";
  $("#averageLatencyMetric").textContent = snapshot.metrics.averageLatencyMs ? `평균 ${formatNumber(snapshot.metrics.averageLatencyMs, 1)} ms` : "평균 —";
  $("#reachMetric").textContent = snapshot.metrics.reachability ? `${formatNumber(snapshot.metrics.reachability * 100, 1)}%` : "—";
  $("#deliveryMetric").textContent = `전달 ${formatNumber(snapshot.metrics.delivered)} · 중복 ${formatNumber(snapshot.metrics.duplicates)}`;
  $("#updatedAt").textContent = new Date(snapshot.generatedAt).toLocaleTimeString("ko-KR");
  renderRuns(snapshot.experiments || []);
  renderAgents(agents);
  renderEvents(snapshot.events || []);
  renderTopology(nodes, edges);
}

function renderRuns(runs) {
  $("#runCount").textContent = runs.length;
  if (!runs.length) {
    $("#runList").innerHTML = '<div class="empty-copy">실행 기록이 없습니다.</div>';
    return;
  }
  $("#runList").innerHTML = runs.map((run) => {
    const progress = run.totalPhases ? Math.round((run.phase / run.totalPhases) * 100) : 0;
    const stop = run.state === "running" ? `<button class="stop-button" data-stop-run="${escapeHTML(run.id)}" type="button">중지</button>` : "";
    return `<article class="run-item">
      <div class="run-title"><strong title="${escapeHTML(run.name)}">${escapeHTML(run.name)}</strong><span class="status-pill ${escapeHTML(run.state)}">${escapeHTML(run.state)}</span></div>
      <div class="run-meta"><span>${escapeHTML(run.phaseName || `seed ${run.seed}`)}</span>${stop}</div>
      <div class="run-meta"><span>job ${formatNumber(run.activeJobs || 0)} 실행 · ${formatNumber(run.completedJobs || 0)} 완료 · ${formatNumber(run.failedJobs || 0)} 실패 · ${formatNumber(run.canceledJobs || 0)} 취소</span></div>
      <div class="progress-track" aria-label="${progress}% 완료"><i style="width:${Math.min(100, progress)}%"></i></div>
      ${run.error ? `<div class="run-meta"><span>${escapeHTML(run.error)}</span></div>` : ""}
    </article>`;
  }).join("");
}

function renderAgents(agents) {
  if (!agents.length) {
    $("#agentRows").innerHTML = '<tr><td colspan="6" class="empty-cell">등록된 에이전트가 없습니다.</td></tr>';
    return;
  }
  $("#agentRows").innerHTML = agents.map((agent) => {
    const usage = agent.capacity ? Math.min(100, Math.round((agent.activeNodes / agent.capacity) * 100)) : 0;
    return `<tr>
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
    $("#eventList").innerHTML = '<li class="empty-copy">수집된 이벤트가 없습니다.</li>';
    return;
  }
  $("#eventList").innerHTML = recent.map((event) => {
    const detail = [event.nodeId, event.remotePeerId ? `← ${event.remotePeerId.slice(0, 10)}` : "", event.latencyMs ? `${formatNumber(event.latencyMs, 1)} ms` : ""].filter(Boolean).join(" · ");
    return `<li class="event-item"><time>${new Date(event.timestamp).toLocaleTimeString("ko-KR", { hour12: false })}</time><span class="event-type">${escapeHTML(event.type)}</span><span class="event-summary" title="${escapeHTML(detail)}">${escapeHTML(detail)}</span></li>`;
  }).join("");
}

function renderTopology(nodes, edges) {
  const svg = $("#topology");
  svg.replaceChildren();
  $("#topologyEmpty").hidden = nodes.length > 0;
  if (!nodes.length) return;
  const width = Math.max(760, svg.clientWidth || 760);
  const height = Math.max(368, svg.clientHeight || 368);
  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  const agents = [...new Set(nodes.map((node) => node.agentId))].sort();
  const positions = new Map();
  agents.forEach((agentId, agentIndex) => {
    const group = nodes.filter((node) => node.agentId === agentId);
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
    label.textContent = agentId;
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
    title.textContent = `${node.id}\n${node.type || node.role} · ${node.state} · ${node.connectedPeers?.length || 0} connections${scoreText}\nPubSub ${pubsubText} · DHT ${dhtText} · ${metadata.topics || "no topics"}`;
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
$("#openScenario").addEventListener("click", () => $("#scenarioDialog").showModal());
$("#runScenario").addEventListener("click", async () => {
  const button = $("#runScenario");
  const error = $("#scenarioError");
  localStorage.setItem("kpl-api-token", $("#apiToken").value.trim());
  button.disabled = true;
  error.textContent = "";
  try {
    const run = await api("/api/v1/experiments", { method: "POST", headers: { "Content-Type": "application/yaml" }, body: $("#scenarioText").value });
    $("#scenarioDialog").close();
    showToast(`${run.name} 실험을 시작했습니다.`);
  } catch (caught) {
    error.textContent = caught.message;
  } finally {
    button.disabled = false;
  }
});

document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-stop-run]");
  if (!button) return;
  try {
    await api(`/api/v1/experiments/${encodeURIComponent(button.dataset.stopRun)}/stop`, { method: "POST" });
    showToast("실험 중지 요청을 보냈습니다.");
  } catch (caught) {
    showToast(caught.message);
  }
});

window.addEventListener("resize", () => state.snapshot && renderTopology(state.snapshot.nodes || [], state.snapshot.edges || []));
connectStream();
