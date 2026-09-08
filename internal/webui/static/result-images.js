/* Per-result PNG previews and downloads; charts use the existing result metrics. */
(function (root) {
  "use strict";
  const colors = ["#2563eb", "#c45c10", "#7c3aed", "#0f766e", "#dc2626", "#7c5b18"];
  const finite = value => typeof value === "number" && Number.isFinite(value);
  const escape = value => String(value ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;", "<":"&lt;", ">":"&gt;", '"':"&quot;", "'":"&#39;"})[c]);
  const number = value => finite(value) ? new Intl.NumberFormat("en-US", {maximumFractionDigits: 2}).format(value) : "N/A";
  const pointOK = point => point && finite(point.x) && finite(point.y);
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

  function chartSVG({title, xLabel, yLabel, series, mode = "line", yMax, zeroX = true, xTicks, source = "", note = ""}) {
    const valid = series.flatMap(s => s.points.filter(pointOK));
    const footer = (source + " · " + note).match(/.{1,90}(?:\s|$)|.{1,90}/g) || [];
    const height = 362 + series.length * 20 + footer.length * 15;
    const label = `<title>${escape(title)}</title>`;
    const begin = `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="${height}" viewBox="0 0 640 ${height}" role="img" aria-label="${escape(title)}" style="background:#ffffff;font:12px system-ui,sans-serif">${label}<rect width="640" height="${height}" fill="#ffffff"/><text x="68" y="17" fill="#142638">${escape(title)}</text>`;
    const foot = footer.map((line, i) => `<text x="28" y="${355+series.length*20+i*15}" fill="#536575" font-size="10">${escape(line)}</text>`).join("");
    if (!valid.length) return begin + '<text x="320" y="160" text-anchor="middle" fill="#536575">No eligible observations available</text>' + foot + '</svg>';
    let xmin = zeroX ? 0 : valid[0].x, xmax = valid[0].x, ymin = 0, ymax = 0;
    for (const point of valid) {
      xmin = Math.min(xmin, point.x); xmax = Math.max(xmax, point.x);
      ymin = Math.min(ymin, point.y); ymax = Math.max(ymax, point.y);
    }
    if (xTicks) { xmin = -.5; xmax = xTicks.length - .5; }
    const xAxis = chartAxis(xmin, xmax);
    const yAxis = chartAxis(ymin, finite(yMax) ? yMax : ymax, finite(yMax) ? 0 : .08);
    const x = v => 68 + xAxis.position(v) * 548;
    const y = v => 266 - yAxis.position(v) * 238;
    const tick = value => value !== 0 && (Math.abs(value) >= 1e9 || Math.abs(value) < .01) ? value.toExponential(2) : number(value);
    const f = value => value.toFixed(2);
    let svg = begin;
    for (let i = 0; i <= 4; i++) {
      const xv = xAxis.value(i / 4), yv = yAxis.value(i / 4);
      svg += `<path d="M68 ${f(266-238*i/4)}H616" stroke="#dce4eb"/><text x="60" y="${f(270-238*i/4)}" fill="#536575" text-anchor="end">${escape(tick(yv))}</text>`;
      if (!xTicks) svg += `<text x="${f(68+548*i/4)}" y="286" fill="#536575" text-anchor="middle">${escape(tick(xv))}</text>`;
    }
    if (xTicks) xTicks.forEach((label, i) => { svg += `<text x="${f(x(i))}" y="286" fill="#536575" text-anchor="middle">${escape(label)}</text>`; });
    svg += `<text x="342" y="310" fill="#34485b" text-anchor="middle">${escape(xLabel)}</text><text transform="translate(16 150) rotate(-90)" fill="#34485b" text-anchor="middle">${escape(yLabel)}</text>`;
    series.forEach((s, index) => {
      const color = colors[index % colors.length];
      let d = "", previous = null;
      const width = Math.max(2, Math.min(xTicks ? 18 : 32, 480 / Math.max(s.points.length, 1)));
      for (const p of s.points) {
        if (!pointOK(p)) { previous = null; continue; }
        const description = `${s.name}: ${number(p.x)} ${xLabel}, ${number(p.y)} ${yLabel}`;
        if (mode === "bar") {
          svg += `<rect x="${f(x(p.x)-width/2)}" y="${f(Math.min(y(0),y(p.y)))}" width="${f(width)}" height="${f(Math.abs(y(0)-y(p.y)))}" fill="${color}"><title>${escape(description)}</title></rect>`;
        } else if (mode !== "scatter") {
          d += previous ? mode === "step" ? `H${f(x(p.x))}V${f(y(p.y))}` : `L${f(x(p.x))} ${f(y(p.y))}` : `M${f(x(p.x))} ${f(y(p.y))}`;
        }
        svg += `<circle cx="${f(x(p.x))}" cy="${f(y(p.y))}" r="${mode === "scatter" ? 6 : 2}" fill="${color}"${mode === "scatter" ? ` tabindex="0" aria-label="${escape(description)}"` : ""}><title>${escape(description)}</title></circle>`;
        previous = p;
      }
      if (d) svg += `<path d="${d}" fill="none" stroke="${color}" stroke-width="2"/>`;
    });
    series.forEach((s, i) => {
      svg += `<rect x="68" y="${326+i*20}" width="9" height="9" fill="${colors[i % colors.length]}"/><text x="84" y="${335+i*20}" fill="#34485b"><title>${escape(s.name)}</title>${escape(s.name.length > 76 ? s.name.slice(0,73) + "…" : s.name)}</text>`;
    });
    return svg + foot + '</svg>';
  }

  function buildCharts(a) {
    validateResponse(a, a?.result?.id);
    const m = a.metrics;
    const source = `${a.result.name || a.result.id} · ${a.result.id} · ${timeLabel(a.asOf)}`;
    const latencyNote = `${number(m.latencySamples)} eligible first receipts; mean ${number(metricValue(m, "averageLatencyMs"))} ms, P95 ${number(metricValue(m, "p95LatencyMs"))} ms. ${m.definition || "Unknown definition"}. Windows: ${(m.deliveryWindows || []).join(", ") || "N/A"}. ${m.measurementIncomplete ? "Incomplete evidence." : ""}`;
    const charts = [
      {id:"latency-cdf",title:"First-delivery latency",xLabel:"Latency (ms)",yLabel:"Eligible samples (%)",mode:"step",yMax:100,
        series:[{name:"Cumulative distribution",points:a.latencyCDF.length ? [{x:0,y:0},...a.latencyCDF.map(p=>({x:p.x,y:p.y*100}))] : []}],
        note:latencyNote + " This is a latency CDF, not network reachability."},
      {id:"latency-distribution",title:"Latency distribution",xLabel:"Latency (ms)",yLabel:"Sample count",mode:"bar",
        series:[{name:"First-delivery samples",points:a.latencyHistogram}],note:latencyNote},
      {id:"message-activity",title:"Message activity",xLabel:"Elapsed time (s)",yLabel:"Observed events / s",mode:"step",
        series:[["publish","Published"],["deliver","Delivered"],["duplicate","Duplicate copies"]].map(([key,name])=>({name,points:trafficPoints(a,key)})),
        note:`Recorded event counts, including local and late receipts; ${a.binSeconds}-second bins. These counts are not delivery ratios.`}
    ];
    const controls = m.gossipsubControl || [];
    if (controls.length) {
      const types = ["ihave","iwant","idontwant","graft","prune"];
      charts.push({id:"gossipsub-control",title:"GossipSub control traffic",xLabel:"Control type",yLabel:"Observed RPC occurrences",mode:"bar",xTicks:types.map(t=>t.toUpperCase()),
        series:["send","recv","drop"].map((direction,i)=>({name:direction,points:types.map((type,x)=>({x:x+(i-1)*.23,y:controls.filter(c=>c.direction===direction && c.controlType===type).reduce((sum,c)=>sum+(finite(c.rpcs)?c.rpcs:0),0)}))})),
        note:"One RPC may contain several control types. Send and receive count separate endpoint observations; drop is a local discard."});
    }
    if (a.bandwidthTimeline?.length) charts.push({id:"p2p-throughput",title:"P2P stream throughput",xLabel:"Elapsed time (s)",yLabel:"kbit/s",mode:"step",
      series:[{name:"Sent",points:bandwidthPoints(a,"*","send")},{name:"Received",points:bandwidthPoints(a,"*","recv")}],
      note:"All libp2p streams; excludes IP/TCP framing, retransmissions and management traffic. Gaps have no bandwidth evidence."});
    const groups = [...new Set(a.observations.flatMap(o=>o.groups.map(g=>g.group)))].filter(Boolean).sort();
    if (!groups.length) groups.push("");
    const byGroup = read => groups.map(group=>({name:group || "All peers",points:observationPoints(a,group,read)})).filter(s=>s.points.some(pointOK));
    const scores = byGroup(g=>g.scoreMean);
    if (scores.length) charts.push({id:"peer-scores",title:"Peer scores by group",xLabel:"Elapsed time (s)",yLabel:"Mean reported score",series:scores,
      note:"Mean of observer-to-peer scores reported by fresh peers in each group. Missing samples are gaps; observers can score the same peer differently."});
    const degrees = byGroup(g=>g.layers.find(l=>l.protocol==="gossipsub")?.averageDegree);
    if (degrees.length) charts.push({id:"mesh-degree",title:"GossipSub mesh degree",xLabel:"Elapsed time (s)",yLabel:"Mean unique neighbors",series:degrees,
      note:"Fresh reported mesh relationships, deduplicated across topics. Each group's degree includes neighbors in other groups."});
    return charts.map(chart=>({...chart,source}));
  }

  function validateResponse(data, id) {
    const record = value => value !== null && typeof value === "object" && !Array.isArray(value);
    const list = (value, check) => Array.isArray(value) && value.every(check);
    const optionalList = (value, check) => value == null || list(value, check);
    const timed = value => record(value) && typeof value.at === "string" && Number.isFinite(Date.parse(value.at));
    if (!id || data?.version !== 1 || data.result?.id !== id || !record(data.metrics) ||
        !list(data.latencyCDF,pointOK) || !list(data.latencyHistogram,pointOK) || !list(data.timeline,timed) ||
        !optionalList(data.metrics.deliveryWindows,v=>typeof v === "string") ||
        !optionalList(data.metrics.gossipsubControl,c=>record(c) && typeof c.controlType === "string") ||
        !optionalList(data.bandwidthTimeline,b=>timed(b) && finite(b.sentBytes) && finite(b.receivedBytes)) ||
        !list(data.observations,o=>timed(o) && list(o.groups,g=>record(g) && typeof g.group === "string" && list(g.layers,l=>record(l) && typeof l.protocol === "string")))) {
      throw new Error("This result contains invalid chart data.");
    }
  }

  // Render the same PNG used by the preview and download. Data URLs work with
  // the Dashboard's existing self/data image policy, without loosening CSP.
  function toPNG(svg, doc, signal) {
    return new Promise((resolve,reject)=>{
      const image = new root.Image();
      const finish = (error,value) => { image.onload = image.onerror = null; signal?.removeEventListener("abort",abort); error ? reject(error) : resolve(value); };
      const abort = () => { image.onload = image.onerror = null; image.src = ""; finish(new DOMException("Image generation canceled", "AbortError")); };
      if (signal?.aborted) { abort(); return; }
      signal?.addEventListener("abort",abort,{once:true});
      image.onerror = () => finish(new Error("Unable to create a graph image. Please retry."));
      image.onload = () => {
        try {
          const canvas = doc.createElement("canvas");
          canvas.width = 1600; canvas.height = Math.round(1600*image.height/image.width);
          const context = canvas.getContext("2d");
          if (!context) throw new Error("This browser cannot create PNG images.");
          context.drawImage(image,0,0,canvas.width,canvas.height);
          finish(null,canvas.toDataURL("image/png"));
        } catch(error) { finish(error); }
      };
      image.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
    });
  }

  function createUI({api,document:doc=root.document,renderImage=(svg,signal)=>toPNG(svg,doc,signal)}) {
    const $ = id => doc.querySelector(`#${id}`);
    const dialog = $("resultImagesDialog");
    let currentID = "", controller = null, revision = 0;
    function status(message,error=false) {
      $("resultImagesStatus").textContent = message;
      $("resultImagesStatus").classList.toggle("error",error);
      $("resultImagesStatus").setAttribute("role",error ? "alert" : "status");
      $("retryResultImages").hidden = !error;
    }
    function cancel() { revision++; controller?.abort(); controller=null; $("resultImagesGrid").innerHTML=""; }
    async function open(id) {
      if (!id || currentID===id && controller) return;
      cancel(); currentID=id;
      const requestRevision=revision, request=new AbortController(); controller=request;
      $("resultImagesName").textContent=id;
      $("resultImagesDate").textContent="";
      status("Creating graph images…");
      dialog.setAttribute("aria-busy","true");
      if (!dialog.open) dialog.showModal();
      const timer=setTimeout(()=>request.abort(),130000);
      try {
        const data=await api(`/api/v1/experiments/${encodeURIComponent(id)}/analysis`,{cache:"no-store",signal:request.signal});
        validateResponse(data,id);
        if (revision!==requestRevision) return;
        $("resultImagesName").textContent=data.result.name || id;
        $("resultImagesDate").textContent=`${data.result.state} · Snapshot ${timeLabel(data.asOf)}`;
        const images=[];
        for (const chart of buildCharts(data)) {
          if (request.signal.aborted) throw new DOMException("Canceled","AbortError");
          const png=await renderImage(chartSVG(chart),request.signal);
          if (revision!==requestRevision) return;
          if (!png.startsWith("data:image/png;base64,")) throw new Error("Unable to create a PNG image.");
          const filename=`${id}-${chart.id}.png`;
          images.push(`<figure class="result-image"><a href="${png}" download="${escape(filename)}" aria-label="${escape(`Download ${chart.title} as PNG`)}"><img src="${png}" alt="${escape(chart.title)}"></a><figcaption><span>${escape(chart.title)}</span><a href="${png}" download="${escape(filename)}">PNG ↓</a></figcaption></figure>`);
        }
        if (request.signal.aborted) throw new DOMException("Canceled","AbortError");
        $("resultImagesGrid").innerHTML=images.join("");
        status(`${images.length} images. Select an image or PNG to download it.${data.observations.length ? "" : " This result has no saved topology or score history."}`);
      } catch(error) {
        if (revision===requestRevision) status(error.name==="AbortError" ? "Image generation timed out. Please retry." : error.message,true);
      } finally {
        clearTimeout(timer);
        if (revision===requestRevision) { controller=null; dialog.setAttribute("aria-busy","false"); }
      }
    }
    $("closeResultImages").addEventListener("click",()=>dialog.close());
    dialog.addEventListener("close",cancel);
    $("retryResultImages").addEventListener("click",()=>{ void open(currentID); });
    return {open,remove:id=>{if(currentID===id) {cancel();currentID="";dialog.close();}}};
  }
  let ui;
  const exported={buildCharts,chartSVG,metricValue,trafficPoints,bandwidthPoints,observationPoints,validateResponse,createUI,
    init:options=>{ui=createUI(options);},open:id=>ui?.open(id),remove:id=>ui?.remove(id)};
  if(typeof module!=="undefined" && module.exports) module.exports=exported;
  else root.KPLResultImages=exported;
})(globalThis);
