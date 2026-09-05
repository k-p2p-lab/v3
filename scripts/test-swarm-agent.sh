#!/bin/sh
# Contract test: multi-network tasks must advertise their Peer overlay IP.
set -eu
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
mkdir "$scratch/bin"
export KPL_TEST_ARGUMENTS="$scratch/arguments"
cat > "$scratch/bin/docker" <<'MOCK_DOCKER'
#!/bin/sh
set -eu
case "$1 $2" in
    'info --format')
        [ "$3" = '{{.Swarm.NodeAddr}}' ] || exit 2
        printf '%s\n' "${KPL_TEST_NODE_ADDR:-192.0.2.9}" ;;
    'inspect --type')
        [ "$3" = container ] && [ "$6" = 'lab_agent.node.task' ] || exit 2
        case "$5" in
            '{{.Image}}') echo sha256:0123456789abcdef ;;
            *) printf 'lab_monitoring 10.1.0.8\n%s %s\n' "$KPL_PEER_NETWORK" "${KPL_TEST_IP:-10.2.0.9}" ;;
        esac ;;
    *) exit 2 ;;
esac
MOCK_DOCKER
cat > "$scratch/bin/kpl" <<'MOCK_KPL'
#!/bin/sh
printf '%s\n' "$@" > "$KPL_TEST_ARGUMENTS"
MOCK_KPL
chmod +x "$scratch/bin/docker" "$scratch/bin/kpl"
export PATH="$scratch/bin:$PATH"
export KPL_SWARM_TASK_NAME=lab_agent.node.task KPL_SWARM_SERVICE_NAME=lab_agent KPL_SWARM_NODE_ID=node
export KPL_PEER_NETWORK=lab-peers KPL_AGENT_CAPACITY=37
sh "$root/scripts/swarm-agent.sh"
grep -qx 'lab_agent-node' "$KPL_TEST_ARGUMENTS"
[ "$(grep -cx 'http://10.2.0.9:8090' "$KPL_TEST_ARGUMENTS")" = 2 ]
grep -qx -- '--metrics-listen' "$KPL_TEST_ARGUMENTS"
grep -qx -- ':9091' "$KPL_TEST_ARGUMENTS"
grep -qx -- 'http://192.0.2.9:9091/metrics' "$KPL_TEST_ARGUMENTS"
grep -qx 'sha256:0123456789abcdef' "$KPL_TEST_ARGUMENTS"
grep -qx '37' "$KPL_TEST_ARGUMENTS"
KPL_AGENT_METRICS_PORT=19091 KPL_TEST_NODE_ADDR='fd00::9' sh "$root/scripts/swarm-agent.sh"
grep -Fqx -- 'http://[fd00::9]:19091/metrics' "$KPL_TEST_ARGUMENTS"
if KPL_AGENT_METRICS_PORT=0 sh "$root/scripts/swarm-agent.sh" 2>/dev/null; then
    echo 'Invalid Agent metrics port was incorrectly admitted' >&2
    exit 1
fi
if KPL_TEST_NODE_ADDR='not an address' sh "$root/scripts/swarm-agent.sh" 2>/dev/null; then
    echo 'Invalid Swarm node address was incorrectly admitted' >&2
    exit 1
fi
if KPL_TEST_IP='fd00::9' sh "$root/scripts/swarm-agent.sh" 2>/dev/null; then
    echo 'IPv6-only task was incorrectly admitted' >&2
    exit 1
fi
agent_stack=$(sed -n '/^  agent:/,/^  prometheus:/p' "$root/stack.swarm.yaml")
printf '%s\n' "$agent_stack" | grep -Fq 'KPL_AGENT_METRICS_PORT: "${KPL_AGENT_METRICS_PORT:-9091}"'
printf '%s\n' "$agent_stack" | grep -Fq 'target: 9091'
if printf '%s\n' "$agent_stack" | grep -Eq 'target: (8090|18000|20000)'; then
    echo 'Agent control or Peer ports were published' >&2
    exit 1
fi
grep -Fq 'source: prometheus-agent-http-sd-v2' "$root/stack.swarm.yaml"
grep -Fq 'http_sd_configs:' "$root/monitoring/prometheus/swarm.yml"
grep -Fq 'honor_labels: true' "$root/monitoring/prometheus/swarm.yml"
if grep -Fq 'tasks.agent' "$root/monitoring/prometheus/swarm.yml"; then
    echo 'Legacy Swarm DNS discovery is still configured' >&2
    exit 1
fi
printf '%s\n' 'PASS: Swarm task identity, overlay address, node-address metrics URL, local image and port validation.'
