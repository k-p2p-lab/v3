#!/bin/sh
# Swarm expands the task/service/node templates in stack.swarm.yaml.
set -eu

fail() { printf 'Swarm Agent: %s\n' "$*" >&2; exit 1; }
: "${KPL_SWARM_TASK_NAME:?Swarm task name is required}"
: "${KPL_SWARM_SERVICE_NAME:?Swarm service name is required}"
: "${KPL_SWARM_NODE_ID:?Swarm node ID is required}"
: "${KPL_PEER_NETWORK:?Peer overlay network is required}"

# Never advertise the service VIP: it can send a node-specific operation to a
# different Agent. Select this task's address on the common Peer network.
addresses=$(docker inspect --type container --format '{{range $name, $net := .NetworkSettings.Networks}}{{printf "%s %s\n" $name $net.IPAddress}}{{end}}' "$KPL_SWARM_TASK_NAME")
address=$(printf '%s\n' "$addresses" | while read -r network ip; do
    if [ "$network" = "$KPL_PEER_NETWORK" ]; then printf '%s\n' "$ip"; fi
done)
case "$address" in
    ''|*[!0-9.]*) fail "No unambiguous IPv4 address on $KPL_PEER_NETWORK" ;;
esac

# Prometheus reaches this task through the node's host-mode published metrics
# port. Keep the node-local control API on the overlay address above.
metrics_port=${KPL_AGENT_METRICS_PORT:-9091}
case "$metrics_port" in
    ''|0*|*[!0-9]*) fail 'KPL_AGENT_METRICS_PORT must be a port between 1 and 65535' ;;
esac
[ "$metrics_port" -le 65535 ] 2>/dev/null || fail 'KPL_AGENT_METRICS_PORT must be a port between 1 and 65535'
node_address=$(docker info --format '{{.Swarm.NodeAddr}}') || fail 'Cannot read this Docker node Swarm address'
case "$node_address" in
    ''|*[!0-9a-fA-F:.]*) fail 'Docker returned an invalid Swarm node address' ;;
esac
metrics_host=$node_address
case "$metrics_host" in *:*) metrics_host=[$metrics_host] ;; esac

# The service task has already pulled this exact image on this worker. Use its
# local content ID so a mutable tag cannot give Peers a different binary.
image=$(docker inspect --type container --format '{{.Image}}' "$KPL_SWARM_TASK_NAME")
case "$image" in sha256:*) ;; *) fail 'Cannot resolve the running service image' ;; esac

exec kpl agent \
    --id "$KPL_SWARM_SERVICE_NAME-$KPL_SWARM_NODE_ID" \
    --name "${KPL_SWARM_NODE_HOSTNAME:-$KPL_SWARM_NODE_ID}" \
    --labels "swarmNodeId=$KPL_SWARM_NODE_ID" \
    --listen :8090 --advertise-url "http://$address:8090" --self-url "http://$address:8090" \
    --metrics-listen :9091 --metrics-url "http://$metrics_host:$metrics_port/metrics" \
    --controller-url "${KPL_CONTROLLER_URL:-http://controller:8080}" \
    --capacity "${KPL_AGENT_CAPACITY:-20}" --data-dir /var/lib/kpl/agent \
    --runtime docker --docker-image "$image" --docker-network "$KPL_PEER_NETWORK"
