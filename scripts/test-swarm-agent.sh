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
[ "$1" = inspect ] && [ "$2" = --type ] && [ "$3" = container ]
[ "$6" = 'lab_agent.node.task' ] || exit 2
case "$5" in
    '{{.Image}}') echo sha256:0123456789abcdef ;;
    *) printf 'lab_monitoring 10.1.0.8\n%s %s\n' "$KPL_PEER_NETWORK" "${KPL_TEST_IP:-10.2.0.9}" ;;
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
grep -qx 'sha256:0123456789abcdef' "$KPL_TEST_ARGUMENTS"
grep -qx '37' "$KPL_TEST_ARGUMENTS"
if KPL_TEST_IP='fd00::9' sh "$root/scripts/swarm-agent.sh" 2>/dev/null; then
    echo 'IPv6-only task was incorrectly admitted' >&2
    exit 1
fi
printf '%s\n' 'PASS: Swarm task identity, overlay address, local image and IPv4 validation.'
