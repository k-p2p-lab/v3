(() => {
  const defaultPorts = { prometheusPort: 9090, grafanaPort: 3000 };

  function validPort(value, fallback) {
    const port = Number(value);
    return Number.isInteger(port) && port > 0 && port <= 65535 ? port : fallback;
  }

  function serviceURL(port, path) {
    const target = new URL(window.location.href);
    target.port = String(port);
    target.pathname = path;
    target.search = "";
    target.hash = "";
    return target.toString();
  }

  function applyLinks(config = defaultPorts) {
    const prometheusPort = validPort(config.prometheusPort, defaultPorts.prometheusPort);
    const grafanaPort = validPort(config.grafanaPort, defaultPorts.grafanaPort);
    document.querySelector("#prometheusLink").href = serviceURL(prometheusPort, "/");
    document.querySelector("#grafanaLink").href = serviceURL(grafanaPort, "/d/kpl-experiments");
  }

  applyLinks();
  fetch("/api/v1/ui-config", { cache: "no-store", headers: { Accept: "application/json" } })
    .then((response) => {
      if (!response.ok) throw new Error(`Dashboard configuration request failed: ${response.status}`);
      return response.json();
    })
    .then(applyLinks)
    .catch(() => {});
})();
