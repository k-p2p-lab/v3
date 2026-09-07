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

  function observationPoints(analysis, group, read) {
    const observations = analysis.observations || [];
    if (!observations.length) return [];
    const origin = Date.parse(observations[0].at);
    return observations.map(observation => {
      const value = observation.groups?.find(item => item.group === group);
      return {x:(Date.parse(observation.at) - origin) / 1000, y:value ? read(value) : null};
    });
  }

  function chartSVG({title, xLabel, yLabel, series, mode = "line", yMax, zeroX = true}) {
    const valid = series.flatMap(s => s.points.filter(pointOK));
    const label = `<title>${escape(title)}</title>`;
    const begin = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 ${350 + series.length * 18}" role="img" aria-label="${escape(title)}" style="background:#0a1620;font:12px system-ui,sans-serif">${label}<rect width="640" height="${350 + series.length * 18}" fill="#0a1620"/><text x="68" y="17" fill="#e9f5f7">${escape(title)}</text>`;
    if (!valid.length) return begin + '<text x="320" y="160" text-anchor="middle" fill="#8ca8b5">No eligible observations available</text></svg>';
    let xmin = zeroX ? 0 : Math.min(...valid.map(p => p.x));
    let xmax = Math.max(...valid.map(p => p.x));
    xmin = Math.min(xmin, ...valid.map(p => p.x));
    if (xmin === xmax) xmax = xmin + Math.max(1, Math.abs(xmin) * .1);
    let ymin = Math.min(0, ...valid.map(p => p.y));
    let ymax = finite(yMax) ? yMax : Math.max(...valid.map(p => p.y), 0);
    if (ymax === ymin) ymax = ymin + 1;
    if (!finite(yMax)) ymax += (ymax - ymin) * .08;
    const x = v => 68 + (v - xmin) / (xmax - xmin) * 548;
    const y = v => 266 - (v - ymin) / (ymax - ymin) * 238;
    const f = value => value.toFixed(2);
    let svg = begin;
    for (let i = 0; i <= 4; i++) {
      const xv = xmin + (xmax - xmin) * i / 4, yv = ymin + (ymax - ymin) * i / 4;
      svg += `<path d="M68 ${f(y(yv))}H616" stroke="#203847"/><text x="60" y="${f(y(yv)+4)}" fill="#8ca8b5" text-anchor="end">${escape(number(yv))}</text>`;
      svg += `<text x="${f(x(xv))}" y="286" fill="#8ca8b5" text-anchor="middle">${escape(number(xv))}</text>`;
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
    const rows = [["runId", "name", "state", "asOf", "definition", "deliveryWindows", ...summaryFields]];
    for (const a of analyses) rows.push([a.result.id, a.result.name, a.result.state, a.asOf, a.metrics.definition, (a.metrics.deliveryWindows || []).join(";"), ...summaryFields.map(key => metricValue(a.metrics, key))]);
    return rows.map(row => row.map(cell).join(",")).join("\r\n") + "\r\n";
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
      $("analysisSummary").innerHTML = `<table aria-label="Analysis summary and baseline differences"><thead><tr><th>Run / snapshot</th><th>Measurement</th><th>Events</th><th>Mean latency</th><th>P95 latency</th><th>Delivery / coverage</th><th>Duplicates / delivery</th><th>Observation quality</th></tr></thead><tbody>${analyses.map(a => {
        const m = a.metrics;
        const bounds = metricRange(m, "reachability", "deliveryRatioUpperBound");
        return `<tr><td>${escape(a.result.name || a.result.id)}<small>${escape(a.result.id)}</small><small>${escape(a.result.state)} · ${escape(timeLabel(a.asOf))}</small></td><td>${escape(m.definition || "N/A")}<small>Windows: ${escape((m.deliveryWindows || []).join(", ") || "N/A")}</small></td><td>${number(m.published)} publish<small>${number(m.delivered)} deliver · ${number(m.duplicates)} duplicate</small></td><td>${number(metricValue(m,"averageLatencyMs"))} ms<small>Δ ${delta(m,"averageLatencyMs")} ms</small></td><td>${number(metricValue(m,"p95LatencyMs"))} ms<small>Δ ${delta(m,"p95LatencyMs")} ms</small></td><td>${bounds}<small>${number(m.eligibleDeliveries)} / ${number(m.expectedDeliveries)} eligible pairs</small><small>Starting delivery: ${metricRange(m,"initialDeliveryRatio","initialDeliveryRatioUpperBound")}</small><small>Stable coverage: ${metricRange(m,"stableCoverage","stableCoverageUpperBound")}</small><small>${number(m.departedPairs)} departed pairs</small></td><td>${number(metricValue(m,"averageDuplicates"))}<small>Δ ${delta(m,"averageDuplicates")}</small></td><td>${number(m.latencySamples)} latency samples<small>${number(m.invalidLatencySamples)} invalid · ${number(m.pendingPublications)} pending</small><small>${number(m.unknownDeliveries)} receipt unknown · ${number(m.continuityUnknownPairs)} continuity unknown</small><small>${number(m.publicationAvailabilityUnknownPairs)} starting availability unknown</small>${m.measurementIncomplete ? "<small>Incomplete measurement evidence</small>" : ""}</td></tr>`;
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
          entry.pending = false; entry.loading = true; entry.error = "";
          entry.controller = new AbortController();
          const timer = setTimeout(() => entry.controller.abort(), 130000);
          status(`Analyzing ${id}…`); render();
          try {
            const data = await api(`/api/v1/experiments/${encodeURIComponent(id)}/analysis`, {cache:"no-store", signal:entry.controller.signal});
            if (data?.version !== 1 || data.result?.id !== id || !data.metrics || ![data.latencyCDF,data.latencyHistogram,data.timeline,data.observations].every(Array.isArray)) throw new Error("Unexpected analysis response.");
            if (entries.get(id) === entry) entry.data = data;
          } catch (error) {
            if (entries.get(id) === entry) entry.error = error.name === "AbortError" ? "Analysis timed out. Use Refresh analysis to retry." : error.message;
          } finally {
            clearTimeout(timer); entry.loading = false; entry.controller = null;
            render();
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
    for (const id of ["analysisGroup", "analysisProtocol"]) $(id).addEventListener("change", () => renderDetail());
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
  const exported = {metricValue, metricRange, difference, trafficPoints, observationPoints, chartSVG, summaryCSV, createUI,
    init: options => { ui = createUI(options); },
    add: id => ui?.add(id), remove: id => ui?.remove(id), setResults: results => ui?.setResults(results)};
  if (typeof module !== "undefined" && module.exports) module.exports = exported;
  else root.KPLAnalysis = exported;
})(globalThis);
