#!/bin/sh
# Fake Docker regression tests; no daemon socket, registry or cluster changes.
set -eu
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
mkdir "$scratch/bin"
export KPL_TEST_CALLS="$scratch/calls"
cat > "$scratch/bin/docker" <<'MOCK_DOCKER'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$KPL_TEST_CALLS"
case "$1 $2" in
    'info --format') printf '%s\n' "${KPL_TEST_MANAGER:-active true}" ;;
    'network inspect') printf '%s\n' "${KPL_TEST_NETWORK:-kpl-swarm-peers overlay true}" ;;
    'node ls')
        [ "$3 $4 $5" = '--quiet --filter node.label=kpl.agent=true' ]
        printf 'worker1\nworker2\nworker3\n' ;;
    'node inspect')
        if [ "$5" = control1 ]; then
            printf '%s\n' "${KPL_TEST_CONTROL:-control1 linux ready active}"
        else
            [ "$5 $6 $7" = 'worker1 worker2 worker3' ]
            printf '%s\n' "${KPL_TEST_AGENTS:-linux ready active
linux ready active
linux down drain}"
        fi ;;
    *) printf 'Unexpected Docker mutation/query: %s\n' "$*" >&2; exit 2 ;;
esac
MOCK_DOCKER
chmod +x "$scratch/bin/docker"
export PATH="$scratch/bin:$PATH"
export KPL_CONTROL_NODE_ID=control1
export KPL_IMAGE=registry.example/kpl:v3@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
export KPL_API_TOKEN=must-not-appear-in-output
unset KPL_PEER_NETWORK KPL_AGENT_CAPACITY

sh "$root/scripts/check-swarm.sh" > "$scratch/output"
grep -q '2 eligible Agents' "$scratch/output"
grep -q 'capacity 20' "$scratch/output"
grep -q 'does not validate' "$scratch/output"
[ "$(wc -l < "$KPL_TEST_CALLS")" -eq 5 ]
if grep -q "$KPL_API_TOKEN" "$scratch/output" "$KPL_TEST_CALLS"; then exit 1; fi

reject() {
    if env "$@" sh "$root/scripts/check-swarm.sh" > "$scratch/output" 2>&1; then
        printf 'Unexpected acceptance: %s\n' "$*" >&2
        exit 1
    fi
}
reject KPL_CONTROL_NODE_ID=
reject KPL_TEST_CONTROL='different1 linux ready active'
reject KPL_TEST_CONTROL='control1 linux ready drain'
reject KPL_TEST_CONTROL='control1 windows ready active'
reject KPL_TEST_MANAGER='active false'
reject KPL_TEST_NETWORK='kpl-swarm-peers bridge true'
reject KPL_TEST_NETWORK='kpl-swarm-peers overlay false'
reject KPL_TEST_AGENTS='linux ready active
linux down active
windows ready active'
reject KPL_AGENT_CAPACITY=0
reject KPL_AGENT_CAPACITY=-1
reject KPL_AGENT_CAPACITY=1.5
reject KPL_AGENT_CAPACITY=08
reject KPL_AGENT_CAPACITY=999999999999999999999999999
reject KPL_IMAGE=registry.example/kpl:latest
reject KPL_IMAGE=registry.example/kpl@sha256:abcd
printf '%s\n' 'PASS: read-only Swarm preflight accepts eligible nodes and rejects invalid manager, control, network, capacity and digest settings.'
