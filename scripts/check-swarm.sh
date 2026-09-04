#!/bin/sh
# Read-only deployment checks on a Swarm manager. No registry login or pull.
set -efu

fail() { printf 'Swarm preflight: %s\n' "$*" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || fail 'Docker CLI is required.'
command -v awk >/dev/null 2>&1 || fail 'awk is required.'
: "${KPL_CONTROL_NODE_ID:?Set KPL_CONTROL_NODE_ID to the exact control node ID}"
: "${KPL_IMAGE:?Set KPL_IMAGE to a registry image pinned by sha256 digest}"
peer_network=${KPL_PEER_NETWORK:-kpl-swarm-peers}
capacity=${KPL_AGENT_CAPACITY:-20}
stack_name=${KPL_STACK_NAME:-kpl}
minimum_agents=${KPL_MIN_AGENTS:-2}

case "$stack_name" in
    ''|[!a-z0-9]*|*[!a-z0-9_-]*) fail 'KPL_STACK_NAME must start with a lowercase letter or digit and contain only lowercase letters, digits, underscores or hyphens.' ;;
esac
agent_label="kpl.$stack_name.agent"
case "$minimum_agents" in
    ''|0*|*[!0-9]*) fail 'KPL_MIN_AGENTS must be a positive decimal integer without leading zeros.' ;;
esac
if ! [ "$minimum_agents" -gt 0 ] 2>/dev/null; then fail 'KPL_MIN_AGENTS exceeds the supported integer range.'; fi

case "$KPL_CONTROL_NODE_ID" in
    -*|*[!a-zA-Z0-9]*) fail 'KPL_CONTROL_NODE_ID must be the exact Swarm node ID.' ;;
esac
case "$peer_network" in
    -*|*[!a-zA-Z0-9_.-]*) fail 'KPL_PEER_NETWORK must be a Docker network name.' ;;
esac
case "$capacity" in
    ''|0*|*[!0-9]*) fail 'KPL_AGENT_CAPACITY must be a positive decimal integer without leading zeros.' ;;
esac
if ! [ "$capacity" -gt 0 ] 2>/dev/null; then fail 'KPL_AGENT_CAPACITY exceeds the supported integer range.'; fi

case "$KPL_IMAGE" in
    *@sha256:*) repository=${KPL_IMAGE%@sha256:*}; digest=${KPL_IMAGE##*@sha256:} ;;
    *) fail 'KPL_IMAGE must use registry/repository@sha256:<64 lowercase hex digits>.' ;;
esac
case "$repository" in
    ''|-*|*://*|*[!a-zA-Z0-9._:/-]*) fail 'KPL_IMAGE has an invalid repository reference.' ;;
esac
case "$digest" in ''|*[!a-f0-9]*) fail 'KPL_IMAGE has an invalid sha256 digest.' ;; esac
[ "${#digest}" -eq 64 ] || fail 'KPL_IMAGE sha256 digest must contain 64 hex digits.'

case "${1:-}" in
    --config-only) [ "$#" -eq 1 ] || fail 'Unexpected arguments.'; exit 0 ;;
    '') [ "$#" -eq 0 ] || fail 'Unexpected arguments.' ;;
    *) fail 'Usage: check-swarm.sh [--config-only]' ;;
esac

swarm=$(docker info --format '{{.Swarm.LocalNodeState}} {{.Swarm.ControlAvailable}}')
[ "$swarm" = 'active true' ] || fail 'Run this check against an active Swarm manager.'

# Inspect only the required fields; avoid dumping task/container inventories.
control=$(docker node inspect --format '{{.ID}} {{.Description.Platform.OS}} {{.Status.State}} {{.Spec.Availability}}' "$KPL_CONTROL_NODE_ID")
[ "$control" = "$KPL_CONTROL_NODE_ID linux ready active" ] || fail 'The exact control node must be Linux, Ready and Active.'
network=$(docker network inspect --format '{{.Name}} {{.Driver}} {{.Attachable}}' "$peer_network")
[ "$network" = "$peer_network overlay true" ] || fail 'The named Peer network must be an attachable overlay.'

agent_ids=$(docker node ls --quiet --filter "node.label=$agent_label=true")
[ -n "$agent_ids" ] || fail "At least $minimum_agents $agent_label=true Linux, Ready, Active nodes are required."
# Word splitting is intentional for Docker-generated IDs; globbing is disabled.
set -- $agent_ids
for id do
    case "$id" in ''|*[!a-zA-Z0-9]*) fail 'Docker returned an invalid node ID.' ;; esac
done
agents=$(docker node inspect --format '{{.Description.Platform.OS}} {{.Status.State}} {{.Spec.Availability}}' "$@")
eligible=$(printf '%s\n' "$agents" | awk '$1 == "linux" && $2 == "ready" && $3 == "active" { n++ } END { print n+0 }')
[ "$eligible" -ge "$minimum_agents" ] || fail "At least $minimum_agents $agent_label=true Linux, Ready, Active nodes are required."

printf 'PASS: Swarm manager, pinned control node, attachable overlay, %s eligible Agents for stack %s, image digest and capacity %s.\n' "$eligible" "$stack_name" "$capacity"
printf '%s\n' 'This checks deployment settings only; it does not validate registry access, physical-node kernels, VXLAN connectivity or capacity under load.'
