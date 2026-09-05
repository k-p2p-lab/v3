// Run with: node --test internal/webui/navigation_test.cjs
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, 'static/navigation.js'), 'utf8');
const markup = fs.readFileSync(path.join(__dirname, 'static/index.html'), 'utf8');

async function renderLinks(href, config) {
  const links = {
    '#prometheusLink': { href: '' },
    '#grafanaLink': { href: '' },
  };
  const requests = [];
  const sandbox = {
    URL,
    window: { location: { href } },
    document: { querySelector: (selector) => links[selector] },
    fetch: async (url, options) => {
      requests.push({ url, options });
      return { ok: true, status: 200, json: async () => config };
    },
  };
  vm.createContext(sandbox);
  vm.runInContext(source, sandbox);
  await new Promise((resolve) => setImmediate(resolve));
  return { links, requests };
}

test('header is named Dashboard and monitoring links open safely in new tabs', () => {
  assert.match(markup, /<title>K-P2PLab Dashboard<\/title>/);
  assert.match(markup, /<p>Dashboard<\/p>/);
  for (const id of ['prometheusLink', 'grafanaLink']) {
    assert.match(markup, new RegExp(`id="${id}"[^>]+target="_blank"[^>]+rel="noopener noreferrer"`));
  }
});

test('monitoring links use the Dashboard host and configured published ports', async () => {
  const { links, requests } = await renderLinks('http://10.20.0.7:18080/dashboard?run=one#top', {
    prometheusPort: 19090,
    grafanaPort: 13000,
  });
  assert.equal(links['#prometheusLink'].href, 'http://10.20.0.7:19090/');
  assert.equal(links['#grafanaLink'].href, 'http://10.20.0.7:13000/d/kpl-experiments');
  assert.equal(requests.length, 1);
  assert.equal(requests[0].url, '/api/v1/ui-config');
  assert.equal(requests[0].options.cache, 'no-store');
  assert.equal(requests[0].options.headers.Accept, 'application/json');
});

test('invalid configuration falls back to default ports and supports IPv6 hosts', async () => {
  const { links } = await renderLinks('https://[2001:db8::7]:18080/', {
    prometheusPort: 0,
    grafanaPort: 70000,
  });
  assert.equal(links['#prometheusLink'].href, 'https://[2001:db8::7]:9090/');
  assert.equal(links['#grafanaLink'].href, 'https://[2001:db8::7]:3000/d/kpl-experiments');
});
