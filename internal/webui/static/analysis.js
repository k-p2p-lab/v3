/* Native SVG charts: no CDN, Python runtime, or external chart service required. */
(function (root) {
  "use strict";
  const colors = ["#39d6d0", "#f1b85a", "#ad9cff", "#83df9a", "#ff7b72"];
  const finite = value => typeof value === "number" && Number.isFinite(value);
  const escape = value => String(value ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;", "<":"&lt;", ">":"&gt;", '"':"&quot;", "'":"&#39;"})[c]);
  const number = value => finite(value) ? new Intl.NumberFormat("en-US", {maximumFractionDigits: 2}).format(value) : "N/A";
  const pointOK = point => point && finite(point.x) && finite(point.y);
  const option = (value, label) => `<option value="${escape(value)}">${escape(label)}</option>`;
  const runLabel = run => `${run.name || run.id} · ${run.id}`;
  const timeLabel = value => value && !String(value).startsWith("0001-") && Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString() : "N/A";

  function metricValue(metrics, key) {
    const m = metrics || {};
    if (["averageLatencyMs", "p95LatencyMs"].includes(key) && !(m.latencySamples > 0)) return null;
    if (key === "averageDuplicates" && !(m.duplicateSamples > 0)) return null;
    if (["reachability", "deliveryRatioUpperBound"].includes(key) && !m.deliveryRatioAvailable) return null;
    if (["initialDeliveryRatio", "initialDeliveryRatioUpperBound"].includes(key) && !m.initialDeliveryRatioAvailable) return null;
    if (["stableCoverage", "stableCoverageUpperBound"].includes(key) && !m.stableCoverageAvailable) return null;
    // Legacy cohort metrics do not define an uncertainty upper bound.
    if (key === "deliveryRatioUpperBound" && m.definition !== "session-window-v1") return null;
    return finite(m[key]) ? m[key] : null;
  }

  function metricRange(metrics, lowerKey, upperKey) {
    const lower = metricValue(metrics, lowerKey), upper = metricValue(metrics, upperKey);
    if (lower == null) return "N/A";
    return `${number(lower * 100)}${upper != null && upper !== lower ? ` – ${number(upper * 100)}` : ""}%`;
  }

  function difference(value, baseline) {
    return finite(value) && finite(baseline) ? value - baseline : null;
  }

  function trafficPoints(analysis, key) {
    const width = analysis.binSeconds;
    if (!(width > 0)) return [];
    const bins = (analysis.timeline || []).filter(bin => Number.isFinite(Date.parse(bin.at))).sort((a,b) => Date.parse(a.at) - Date.parse(b.at));
    if (!bins.length) return [];
    const origin = Date.parse(bins[0].at);
    const points = [];
    let end = 0;
    for (const bin of bins) {
      const x = (Date.parse(bin.at) - origin) / 1000;
      if (x > end) points.push({x:end, y:0}, {x, y:0});
      points.push({x, y:finite(bin[key]) ? bin[key] / width : null});
      end = x + width;
    }
    const last = points[points.length - 1];
    points.push({x:end, y:last.y});
    return points;
  }

  // The API allocates bytes uniformly over each measured interval. Divide by
  // the display bin width only for throughput; cumulative totals retain bytes.
  function bandwidthPoints(analysis, protocol, direction, cumulative = false) {
    const width = analysis.bandwidthBinSeconds;
    const bins = analysis.bandwidthTimeline || [];
    if (!(width > 0) || !bins.length) return [];
    const origin = Date.parse(bins[0].at), key = direction === "send" ? "sentBytes" : "receivedBytes";
    let total = 0, end = 0;
    const points = [];
    for (const bin of bins) {
      const x = (Date.parse(bin.at) - origin) / 1000;
      if (!Number.isFinite(x)) continue;
      if (x > end) points.push({x:end,y:null});
      const entry = protocol === "*" ? bin : bin.protocols?.find(p => p.protocol === protocol);
      const bytes = entry?.[key] ?? 0;
      if (cumulative) {
        points.push({x,y:total / 1024}); total += bytes;
        points.push({x:x + width,y:total / 1024});
      } else {
        points.push({x,y:bytes * 8 / width / 1000}, {x:x + width,y:bytes * 8 / width / 1000});
      }
      end = x + width;
    }
    return points;
  }

  function observationPoints(analysis, group, read) {
    const observations = analysis.observations || [];
    if (!observations.length) return [];
    const origin = Date.parse(observations[0].at);
    return observations.map(observation => {
      const value = observation.groups?.find(item => item.group === group);
      return {x:(Date.parse(observation.at) - origin) / 1000, y:value ? read(value) : null};
    });
  }

  // Normalize before subtracting: two finite endpoints can have an infinite
  // difference. Keep constant axes and padding within the finite number range.
  function chartAxis(low, high, padding = 0) {
    if (low === high) {
      const step = Math.max(1, Math.abs(low) * .1);
      if (finite(high + step)) high += step;
      else low -= step;
    }
    const scale = Math.max(Math.abs(low), Math.abs(high));
    const min = low / scale;
    let max = high / scale;
    const padded = max + (max - min) * padding;
    if (finite(padded * scale)) max = padded;
    return {
      position: value => (value / scale - min) / (max - min),
      value: fraction => (min * (1 - fraction) + max * fraction) * scale
    };
  }

  function chartSVG({title, xLabel, yLabel, series, mode = "line", yMax, zeroX = true}) {
    const valid = series.flatMap(s => s.points.filter(pointOK));
    const label = `<title>${escape(title)}</title>`;
    const begin = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 ${350 + series.length * 18}" role="img" aria-label="${escape(title)}" style="background:#0a1620;font:12px system-ui,sans-serif">${label}<rect width="640" height="${350 + series.length * 18}" fill="#0a1620"/><text x="68" y="17" fill="#e9f5f7">${escape(title)}</text>`;
    if (!valid.length) return begin + '<text x="320" y="160" text-anchor="middle" fill="#8ca8b5">No eligible observations available</text></svg>';
    let xmin = zeroX ? 0 : valid[0].x, xmax = valid[0].x, ymin = 0, ymax = 0;
    for (const point of valid) {
      xmin = Math.min(xmin, point.x); xmax = Math.max(xmax, point.x);
      ymin = Math.min(ymin, point.y); ymax = Math.max(ymax, point.y);
    }
    const xAxis = chartAxis(xmin, xmax);
    const yAxis = chartAxis(ymin, finite(yMax) ? yMax : ymax, finite(yMax) ? 0 : .08);
    const x = v => 68 + xAxis.position(v) * 548;
    const y = v => 266 - yAxis.position(v) * 238;
    const tick = value => value !== 0 && (Math.abs(value) >= 1e9 || Math.abs(value) < .01) ? value.toExponential(2) : number(value);
    const f = value => value.toFixed(2);
    let svg = begin;
    for (let i = 0; i <= 4; i++) {
      const xv = xAxis.value(i / 4), yv = yAxis.value(i / 4);
      svg += `<path d="M68 ${f(266-238*i/4)}H616" stroke="#203847"/><text x="60" y="${f(270-238*i/4)}" fill="#8ca8b5" text-anchor="end">${escape(tick(yv))}</text>`;
      svg += `<text x="${f(68+548*i/4)}" y="286" fill="#8ca8b5" text-anchor="middle">${escape(tick(xv))}</text>`;
    }
    svg += `<text x="342" y="310" fill="#b7ccd5" text-anchor="middle">${escape(xLabel)}</text><text transform="translate(16 150) rotate(-90)" fill="#b7ccd5" text-anchor="middle">${escape(yLabel)}</text>`;
    series.forEach((s, index) => {
      const color = colors[index % colors.length];
      let d = "", previous = null;
      const width = Math.max(2, Math.min(32, 480 / Math.max(s.points.length, 1)));
      for (const p of s.points) {
        if (!pointOK(p)) { previous = null; continue; }
        const description = `${s.name}: ${number(p.x)} ${xLabel}, ${number(p.y)} ${yLabel}`;
        if (mode === "bar") {
          svg += `<rect x="${f(x(p.x)-width/2)}" y="${f(Math.min(y(0),y(p.y)))}" width="${f(width)}" height="${f(Math.abs(y(0)-y(p.y)))}" fill="${color}"><title>${escape(description)}</title></rect>`;
        } else if (mode !== "scatter") {
          d += previous ? mode === "step" ? `H${f(x(p.x))}V${f(y(p.y))}` : `L${f(x(p.x))} ${f(y(p.y))}` : `M${f(x(p.x))} ${f(y(p.y))}`;
        }
        // A native tooltip is also available to keyboard users for scatter points.
        svg += `<circle cx="${f(x(p.x))}" cy="${f(y(p.y))}" r="${mode === "scatter" ? 6 : 2}" fill="${color}"${mode === "scatter" ? ` tabindex="0" aria-label="${escape(description)}"` : ""}><title>${escape(description)}</title></circle>`;
        previous = p;
      }
      if (d) svg += `<path d="${d}" fill="none" stroke="${color}" stroke-width="2"/>`;
    });
    series.forEach((s, i) => {
      svg += `<rect x="68" y="${326+i*18}" width="9" height="9" fill="${colors[i % colors.length]}"/><text x="84" y="${335+i*18}" fill="#b7ccd5"><title>${escape(s.name)}</title>${escape(s.name.length > 76 ? s.name.slice(0,73) + "…" : s.name)}</text>`;
    });
    return svg + '</svg>';
  }

  function chartMarkup(chart) {
    return `<figure class="analysis-chart"><header><h3>${escape(chart.title)}</h3><button type="button" data-export-chart="${escape(chart.title)}" aria-label="${escape(`Download ${chart.title} as SVG`)}">SVG</button></header>${chartSVG(chart)}<figcaption>${escape(chart.note || "Hover over a point to inspect its value.")}</figcaption></figure>`;
  }

  const summaryFields = ["published", "delivered", "duplicates", "averageLatencyMs", "p95LatencyMs", "latencySamples", "reachability", "deliveryRatioUpperBound", "averageDuplicates", "duplicateSamples", "pendingPublications", "invalidLatencySamples", "unknownDeliveries", "continuityUnknownPairs", "publicationAvailabilityUnknownPairs", "initialExpectedDeliveries", "initialEligibleDeliveries", "initialUnknownDeliveries", "initialDeliveryRatio", "initialDeliveryRatioUpperBound", "stableCoverage", "stableCoverageUpperBound", "departedPairs"];
  function summaryCSV(analyses) {
    const cell = value => {
      let str = value == null ? "" : String(value);
      if (typeof value === "string" && /^[=+\-@\t\r\n]/.test(str)) str = "'" + str;
      return '"' + str.replace(/"/g, '""') + '"';
    };
    const rows = [["runId", "name", "state", "asOf", "definition", "deliveryWindows", ...summaryFields, "bandwidthScope", "bandwidthReceivedBytes", "bandwidthSentBytes", "bandwidthSessions", "bandwidthFinalizedSessions", "bandwidthRejectedSamples"]];
    for (const a of analyses) rows.push([a.result.id, a.result.name, a.result.state, a.asOf, a.metrics.definition, (a.metrics.deliveryWindows || []).join(";"), ...summaryFields.map(key => metricValue(a.metrics, key)), a.metrics.bandwidth?.scope, a.metrics.bandwidth?.sessions ? a.metrics.bandwidth.receivedBytes : null, a.metrics.bandwidth?.sessions ? a.metrics.bandwidth.sentBytes : null, a.metrics.bandwidth?.sessions, a.metrics.bandwidth?.finalizedSessions, a.metrics.bandwidth?.rejectedSamples]);
    return rows.map(row => row.map(cell).join(",")).join("\r\n") + "\r\n";
  }

  // Validate every run before storing it, including detail data that is not
  // currently rendered. Changing a run, group or protocol must remain safe.
  function validateAnalysisResponse(data, id) {
    const record = value => value !== null && typeof value === "object" && !Array.isArray(value);
    const list = (value, check) => Array.isArray(value) && value.every(check);
    const optionalList = (value, check) => value == null || list(value, check);
    const string = value => typeof value === "string";
    const timed = value => record(value) && string(value.at);
    const protocol = value => record(value) && string(value.protocol) && finite(value.sentBytes) && finite(value.receivedBytes);
    if (data?.version !== 1 || data.result?.id !== id || !record(data.metrics) ||
        ![data.latencyCDF,data.latencyHistogram,data.timeline,data.observations].every(Array.isArray)) {
      throw new Error("Unexpected analysis response.");
    }
    const m = data.metrics;
    if (!list(data.latencyCDF, pointOK) || !list(data.latencyHistogram, pointOK) || !list(data.timeline, timed) ||
        !optionalList(m.deliveryWindows, string) ||
        !optionalList(m.gossipsubControl, c => record(c) && string(c.controlType)) ||
        (m.bandwidth != null && (!record(m.bandwidth) || !optionalList(m.bandwidth.protocols, protocol))) ||
        !optionalList(data.bandwidthTimeline, b => timed(b) && finite(b.sentBytes) && finite(b.receivedBytes) && optionalList(b.protocols, protocol)) ||
        !list(data.observations, o => timed(o) && list(o.groups, g => record(g) && string(g.group) &&
          list(g.layers, l => record(l) && string(l.protocol) && list(l.degrees, pointOK))))) {
      throw new Error("Unexpected analysis response: invalid chart data.");
    }
  }

  function createUI({api, document: doc = root.document}) {
    const $ = id => doc.querySelector(`#${id}`);
    let results = [], entries = new Map(), working = false, detailID = "";
    const selected = () => [...entries.values()].filter(entry => entry.data).map(entry => entry.data);
    const busy = () => [...entries.values()].some(entry => entry.pending || entry.loading);
    const status = (message, error = false) => {
      $("analysisStatus").textContent = message;
      $("analysisStatus").classList.toggle("error", error);
      $("analysisStatus").setAttribute("role", error ? "alert" : "status");
    };
    function download(content, type, name) {
      const url = URL.createObjectURL(new Blob([content], {type}));
      const link = doc.createElement("a");
      link.href = url; link.download = name;
      doc.body.appendChild(link); link.click(); link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    }
    function updateChoices() {
      const current = $("analysisRun").value;
      $("analysisRun").innerHTML = option("", "Select a saved run") + results.filter(run => !entries.has(run.id) && !["unreadable", "queued"].includes(run.state)).map(run => option(run.id, runLabel(run))).join("");
      if (results.some(run => run.id === current && !entries.has(run.id))) $("analysisRun").value = current;
      $("addAnalysis").disabled = !$("analysisRun").value || entries.size >= 4;
    }
    function render() {
      updateChoices();
      $("refreshAnalysis").disabled = !entries.size || busy();
      $("analysisRuns").innerHTML = [...entries].map(([id, entry], i) => `<li><span>${i === 0 ? "Baseline · " : ""}${escape(runLabel(entry.data?.result || results.find(run => run.id === id) || {id}))}${entry.loading ? " · Analyzing…" : entry.pending ? " · Queued" : ""}${entry.error ? ` · ${escape(entry.error)}` : ""}</span><button type="button" data-remove-analysis="${escape(id)}" aria-label="${escape(`Remove ${id} from analysis`)}">Remove</button></li>`).join("");
      const analyses = selected();
      $("analysisContent").hidden = !analyses.length;
      $("exportAnalysis").disabled = $("exportAnalysisCSV").disabled = !analyses.length || busy();
      if (!analyses.length) return;
      const baseline = entries.values().next().value?.data;
      const delta = (m, key) => number(difference(metricValue(m, key), metricValue(baseline?.metrics, key)));
      $("analysisSummary").innerHTML = `<table aria-label="Analysis summary and baseline differences"><thead><tr><th>Run / snapshot</th><th>Measurement</th><th>Events</th><th>Mean latency</th><th>P95 latency</th><th>Delivery / coverage</th><th>Duplicates / delivery</th><th>P2P stream transfer</th><th>Observation quality</th></tr></thead><tbody>${analyses.map(a => {
        const m = a.metrics;
        const bounds = metricRange(m, "reachability", "deliveryRatioUpperBound");
        return `<tr><td>${escape(a.result.name || a.result.id)}<small>${escape(a.result.id)}</small><small>${escape(a.result.state)} · ${escape(timeLabel(a.asOf))}</small></td><td>${escape(m.definition || "N/A")}<small>Windows: ${escape((m.deliveryWindows || []).join(", ") || "N/A")}</small></td><td>${number(m.published)} publish<small>${number(m.delivered)} deliver · ${number(m.duplicates)} duplicate</small></td><td>${number(metricValue(m,"averageLatencyMs"))} ms<small>Δ ${delta(m,"averageLatencyMs")} ms</small></td><td>${number(metricValue(m,"p95LatencyMs"))} ms<small>Δ ${delta(m,"p95LatencyMs")} ms</small></td><td>${bounds}<small>${number(m.eligibleDeliveries)} / ${number(m.expectedDeliveries)} eligible pairs</small><small>Starting delivery: ${metricRange(m,"initialDeliveryRatio","initialDeliveryRatioUpperBound")}</small><small>Stable coverage: ${metricRange(m,"stableCoverage","stableCoverageUpperBound")}</small><small>${number(m.departedPairs)} departed pairs</small></td><td>${number(metricValue(m,"averageDuplicates"))}<small>Δ ${delta(m,"averageDuplicates")}</small></td><td>${m.bandwidth?.sessions ? `${number(m.bandwidth.sentBytes / 1024)} KiB sent<small>${number(m.bandwidth.receivedBytes / 1024)} KiB received</small><small>${number(m.bandwidth.finalizedSessions)} / ${number(m.bandwidth.sessions)} sessions finalized</small>` : "N/A"}</td><td>${number(m.latencySamples)} latency samples<small>${number(m.invalidLatencySamples)} invalid · ${number(m.pendingPublications)} pending</small><small>${number(m.unknownDeliveries)} receipt unknown · ${number(m.continuityUnknownPairs)} continuity unknown</small><small>${number(m.publicationAvailabilityUnknownPairs)} starting availability unknown</small>${m.measurementIncomplete ? "<small>Incomplete measurement evidence</small>" : ""}</td></tr>`;
      }).join("")}</tbody></table>`;
      $("analysisComparisonCharts").innerHTML = [
        {title:"Propagation latency CDF", xLabel:"Latency (ms)", yLabel:"Eligible samples (%)", mode:"step", yMax:100,
          series:analyses.map(a => ({name:runLabel(a.result), points:a.latencyCDF.length ? [{x:0,y:0}, ...a.latencyCDF.map(p => ({x:p.x,y:p.y*100}))] : []})),
          note:"Empirical CDF of eligible first deliveries. Curves are reduced to about 200 points; missing and invalid latency samples are excluded."},
        {title:"Latency / duplicate trade-off", xLabel:"Mean latency (ms)", yLabel:"Duplicates / delivery", mode:"scatter",
          series:analyses.map(a => ({name:runLabel(a.result), points:[{x:metricValue(a.metrics,"averageLatencyMs"),y:metricValue(a.metrics,"averageDuplicates") }]})),
          note:"Each point is one run. Lower latency and fewer observed duplicate copies are preferable; assess delivery and coverage bounds and evidence quality in the table too."}
      ].map(chartMarkup).join("");
      if (!analyses.some(a => a.result.id === detailID)) detailID = analyses[0].result.id;
      $("analysisDetailRun").innerHTML = analyses.map(a => option(a.result.id, runLabel(a.result))).join("");
      $("analysisDetailRun").value = detailID;
      renderDetail();
    }
    function renderDetail(resetSample = false) {
      const a = entries.get(detailID)?.data;
      if (!a) return;
      const observations = a.observations || [];
      const groups = [...new Set(observations.flatMap(o => o.groups.map(g => g.group)))].filter(Boolean).sort();
      const previous = $("analysisGroup").value;
      $("analysisGroup").innerHTML = option("", "All groups") + groups.map(g => option(g,g)).join("");
      $("analysisGroup").value = groups.includes(previous) ? previous : "";
      const group = $("analysisGroup").value, protocol = $("analysisProtocol").value;
      const slider = $("analysisSample");
      const atEnd = Number(slider.value) === Number(slider.max);
      slider.max = Math.max(0, observations.length - 1);
      if (resetSample || atEnd) slider.value = slider.max;
      slider.disabled = !observations.length;
      const observation = observations[Number(slider.value)];
      $("analysisSampleTime").textContent = timeLabel(observation?.at);
      $("analysisObservationNote").textContent = observations.length
        ? `${number(observations.length)} displayed / ${number(a.observationCount)} stored observations (normally every 5 s). Group and layer filters apply to topology and score charts. Degrees count unique neighbors in the full run, across all topics; only fresh reporting peers enter topology statistics. Null values are gaps. Clustering is N/A when the exact calculation exceeds its work limit.`
        : "This result has no stored topology or score observations. Event and latency charts remain available. New runs record observations on the Controller every 5 seconds, independently of this page.";
      const bandwidth = a.metrics.bandwidth;
      const protocols = (bandwidth?.protocols || []).map(p => p.protocol);
      const selector = $("analysisBandwidthProtocol"), previousProtocol = selector.value;
      selector.innerHTML = option("*", "All libp2p streams") + protocols.map(p => option(p,p || "Unassigned / negotiation")).join("");
      selector.value = protocols.includes(previousProtocol) ? previousProtocol : "*";
      selector.disabled = !bandwidth?.sessions;
      const bandwidthProtocol = selector.value;
      $("analysisBandwidthNote").textContent = bandwidth?.sessions
        ? `Measured libp2p stream usage: ${number(bandwidth.sentBytes)} bytes sent; ${number(bandwidth.receivedBytes)} bytes received. ${number(bandwidth.finalizedSessions)} / ${number(bandwidth.sessions)} reporting sessions finalized; missing final samples leave unknown tails. ${number(bandwidth.samples)} accepted, ${number(bandwidth.rejectedSamples)} invalid, ${number(bandwidth.supersededSamples)} older samples. Reported peers only; completely missing peers cannot be counted. Charts average intervals into ${a.bandwidthBinSeconds} s bins; partial bins use the full bin width. Gaps are unknown. Protocol filter applies only to these two charts. This measures usage, not link capacity; HTTP management, TCP/IP, encryption, framing and retransmission overhead are excluded. Send and receive count opposite endpoints separately; do not add them as unique network traffic.`
        : `Bandwidth N/A: this result has no valid stream-counter samples${bandwidth?.rejectedSamples ? ` (${bandwidth.rejectedSamples} invalid samples)` : ""}. Rebuild all v3 components and start a new run to collect them. Message payload sizes cannot recover historical P2P bandwidth.`;
      const controls = a.metrics.gossipsubControl || [];
      $("analysisControlSummary").innerHTML = controls.length
        ? `<table aria-label="GossipSub control breakdown"><thead><tr><th>Agent</th><th>Direction</th><th>Control</th><th>RPC occurrences</th><th>Entries</th><th>Message ID references</th><th>Peer exchange records</th></tr></thead><tbody>${controls.map(c => `<tr><td>${escape(c.agentId || "Unassigned")}</td><td>${escape(c.direction)}</td><td>${escape(c.controlType.toUpperCase())}</td><td>${number(c.rpcs)}</td><td>${number(c.entries)}</td><td>${number(c.messageIds)}</td><td>${number(c.peerExchangeRecords)}</td></tr>`).join("")}</tbody></table>`
        : '<p class="results-help">No recorded GossipSub control traffic.</p>';
      $("analysisBandwidthCharts").innerHTML = [
        {title:"P2P stream throughput",xLabel:"Seconds since first bandwidth bin",yLabel:"kbit/s",mode:"step",series:["send","receive"].map(direction => ({name:`${direction} · ${bandwidthProtocol === "*" ? "All streams" : bandwidthProtocol || "Unassigned / negotiation"} · ${runLabel(a.result)}`,points:bandwidthPoints(a,bandwidthProtocol,direction)})),note:"Cumulative counter differences / source monotonic elapsed time, averaged into display bins. Intervals spanning missing samples are longer averages. Physical link capacity is not measured."},
        {title:"Cumulative P2P stream transfer",xLabel:"Seconds since first bandwidth bin",yLabel:"KiB",series:["send","receive"].map(direction => ({name:`${direction} · ${bandwidthProtocol === "*" ? "All streams" : bandwidthProtocol || "Unassigned / negotiation"} · ${runLabel(a.result)}`,points:bandwidthPoints(a,bandwidthProtocol,direction,true)})),note:"Recorded bytes across reporting Peer sessions; includes forwarding and protocol control traffic. Missing final samples can undercount a stopped Peer. Protocol attribution may briefly lag the total while a stream operation completes."}
      ].map(chartMarkup).join("");
      const traffic = (key, name) => ({name, points:trafficPoints(a, key)});
      const obs = (key, name) => ({name, points:observationPoints(a,group,g => g[key])});
      const layer = (key, name) => ({name, points:observationPoints(a,group,g => g.layers.find(l => l.protocol === protocol)?.[key])});
      const latestLayer = observation?.groups.find(g => g.group === group)?.layers.find(l => l.protocol === protocol);
      const charts = [
        {title:"Latency histogram",xLabel:"Latency bin center (ms)",yLabel:"Samples",mode:"bar",series:[{name:"Eligible first deliveries",points:a.latencyHistogram}]},
        {title:"Message event rate",xLabel:"Seconds since first event bin",yLabel:"Events / s",mode:"step",series:[traffic("publish","Publish"),traffic("deliver","Deliver"),traffic("duplicate","Duplicate")],note:`Raw observed events, including deliveries outside the eligible cohort. Bins: ${a.binSeconds} s. ${a.untimedEvents} untimed events omitted from the timeline.`},
        {title:"GossipSub control rate",xLabel:"Seconds since first event bin",yLabel:"Control events / s",mode:"step",series:[traffic("send","Send"),traffic("recv","Receive"),traffic("drop","Drop")],note:"Counts recorded control event types (IHAVE, IWANT, IDONTWANT, GRAFT, PRUNE). One wire RPC can carry several control types."},
        {title:"Peer lifecycle",xLabel:"Seconds since first observation",yLabel:"Peers",series:[obs("ready","Ready"),obs("starting","Starting"),obs("stopping","Stopping"),obs("reporting","Fresh reports"),obs("failed","Failed")]},
        {title:"Average degree",xLabel:"Seconds since first observation",yLabel:"Unique neighbors / peer",series:[layer("averageDegree",protocol)]},
        {title:"Clustering coefficient",xLabel:"Seconds since first observation",yLabel:"Mean local clustering",yMax:1,series:[layer("clustering",protocol)]},
        {title:"Degree distribution",xLabel:"Unique neighbors",yLabel:"Peers (%)",mode:"bar",yMax:100,series:[{name:protocol,points:latestLayer?.nodes > 0 ? latestLayer.degrees.map(p => ({x:p.x,y:100*p.y/latestLayer.nodes})) : []}],note:`Selected observation: ${timeLabel(observation?.at)}. ${latestLayer?.nodes || 0} eligible peers. Adjust the sample slider to inspect another time.`},
        {title:"Peer score",xLabel:"Seconds since first observation",yLabel:"Reported score",series:[obs("scoreMean","Mean"),obs("scoreMin","Minimum"),obs("scoreMax","Maximum")],note:"Scores are observer-to-peer reports, not a global score assigned to each peer. Averages weight each reported score equally."},
        {title:"Negative peer scores",xLabel:"Seconds since first observation",yLabel:"Reports with score < 0 (%)",yMax:100,series:[{name:"Negative scores",points:observationPoints(a,group,g => finite(g.negativeScoreRatio) ? g.negativeScoreRatio*100 : null)}]},
      ];
      $("analysisDetailCharts").innerHTML = charts.map(chartMarkup).join("");
    }
    async function drain() {
      if (working) return;
      working = true;
      try {
        for (;;) {
          const next = [...entries].find(([, entry]) => entry.pending);
          if (!next) break;
          const [id, entry] = next;
          const previousData = entry.data;
          entry.pending = false; entry.loading = true; entry.error = "";
          entry.controller = new AbortController();
          const timer = setTimeout(() => entry.controller.abort(), 130000);
          status(`Analyzing ${id}…`); render();
          try {
            const data = await api(`/api/v1/experiments/${encodeURIComponent(id)}/analysis`, {cache:"no-store", signal:entry.controller.signal});
            validateAnalysisResponse(data, id);
            if (entries.get(id) === entry) entry.data = data;
          } catch (error) {
            if (entries.get(id) === entry) entry.error = error.name === "AbortError" ? "Analysis timed out. Use Refresh analysis to retry." : error.message;
          } finally {
            clearTimeout(timer); entry.loading = false; entry.controller = null;
            try {
              render();
            } catch {
              // A valid outer envelope can still contain corrupt nested chart
              // data. Roll back before rendering again so refresh remains usable.
              entry.data = previousData;
              entry.error = "Unexpected analysis response: invalid chart data.";
              render();
            }
          }
        }
      } finally {
        working = false;
        const errors = [...entries.values()].filter(e => e.error);
        status(errors.length ? "Some runs could not be analyzed. See the selected runs above; use Refresh analysis to retry. Previously loaded snapshots remain visible." : entries.size ? "Analysis snapshots loaded. Use Refresh analysis to include new events and observations." : "Choose Analyze in Saved results, or add a run above.", !!errors.length);
      }
    }
    function add(id) {
      if (!id) return;
      $("analysisPanel").scrollIntoView?.({block:"start"});
      if (entries.has(id)) { detailID = id; render(); return; }
      if (entries.size >= 4) { status("Remove a selected run before adding another (maximum four)."); return; }
      entries.set(id, {pending:true, loading:false, data:null, error:""});
      detailID = id; render(); void drain();
    }
    function remove(id) {
      const entry = entries.get(id);
      if (!entry) return;
      entries.delete(id); entry.controller?.abort(); render();
      if (!entries.size) status("Choose Analyze in Saved results, or add a run above.");
    }
    function setResults(value) {
      results = value;
      for (const id of [...entries.keys()]) if (!results.some(run => run.id === id)) remove(id);
      updateChoices();
    }
    $("addAnalysis").addEventListener("click", () => add($("analysisRun").value));
    $("analysisRun").addEventListener("change", updateChoices);
    $("refreshAnalysis").addEventListener("click", () => {
      if (busy()) return;
      for (const entry of entries.values()) entry.pending = true;
      render(); void drain();
    });
    $("analysisDetailRun").addEventListener("change", () => { detailID = $("analysisDetailRun").value; renderDetail(true); });
    for (const id of ["analysisGroup", "analysisProtocol", "analysisBandwidthProtocol"]) $(id).addEventListener("change", () => renderDetail());
    $("analysisSample").addEventListener("input", () => renderDetail());
    $("analysisRuns").addEventListener("click", event => {
      const button = event.target.closest("[data-remove-analysis]");
      if (button) remove(button.dataset.removeAnalysis);
    });
    $("analysisPanel").addEventListener("click", event => {
      const button = event.target.closest("[data-export-chart]");
      if (button) {
        const svg = button.closest("figure").querySelector("svg");
        download(svg.outerHTML, "image/svg+xml", button.dataset.exportChart.replace(/[^a-z0-9]+/gi,"-").toLowerCase() + ".svg");
      }
    });
    $("exportAnalysis").addEventListener("click", () => download(JSON.stringify({version:1,runs:selected()},null,2), "application/json", "kpl-analysis.json"));
    $("exportAnalysisCSV").addEventListener("click", () => download(summaryCSV(selected()), "text/csv;charset=utf-8", "kpl-analysis-summary.csv"));
    return {add, remove, setResults};
  }

  let ui;
  const exported = {metricValue, metricRange, difference, trafficPoints, bandwidthPoints, observationPoints, chartSVG, summaryCSV, createUI,
    init: options => { ui = createUI(options); },
    add: id => ui?.add(id), remove: id => ui?.remove(id), setResults: results => ui?.setResults(results)};
  if (typeof module !== "undefined" && module.exports) module.exports = exported;
  else root.KPLAnalysis = exported;
})(globalThis);
