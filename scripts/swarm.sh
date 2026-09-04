#!/bin/sh
# Manager-side lifecycle commands. Never sources the credentials file.
set -efu

fail() { printf 'KPL Swarm: %s\n' "$*" >&2; exit 1; }
usage() {
    cat <<'EOF'
Usage: sh scripts/swarm.sh [--env-file PATH] COMMAND [NODE... | SELECTOR]
  init                 Create a private .env.swarm with random credentials
  deploy [NODE... | SELECTOR]  Deploy; bare deploy reuses existing Agent labels
  status               Show services, Agent tasks and selected nodes
  add-node NODE... | SELECTOR     Enable one Agent per selected Linux node
  remove-node NODE... | SELECTOR  Stop Agents and wait for their Peer cleanup
  remove               Stop Controller, then Agents, then remove stack services
Environment overrides the config file. NODE is a Swarm node ID or hostname.
Use one selector without NODE arguments:
  --all                 All nodes, including managers
  --all-excluding-self  All except the current Docker daemon's Swarm node
  --workers             Worker nodes only
Deploy/add selectors skip nodes that are not Linux, Ready and Active.
Remove selectors target only this stack's labeled Agents; unavailable targets fail.
Selection uses the current cluster snapshot, not nodes that join later.
KPL_IMAGE accepts an explicit tag or digest; deploy resolves tags without editing the file.
KPL_IMAGE_PULL_TIMEOUT sets the tag pull timeout in seconds (default: 300).
Removal preserves experiment/monitoring volumes and the external Peer network.
EOF
}

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
env_file=$root/.env.swarm
explicit_env_file=no
if [ "${1:-}" = --env-file ]; then
    [ "$#" -ge 2 ] || fail '--env-file needs a path.'
    env_file=$2
    explicit_env_file=yes
    shift 2
fi
command_name=${1:-help}
[ "$#" -eq 0 ] || shift
case "$command_name" in
    help|-h|--help) usage; exit 0 ;;
    init|deploy|status|add-node|remove-node|remove) ;;
    *) usage >&2; fail "Unknown command: $command_name" ;;
esac
case "$command_name" in
    init|status|remove) [ "$#" -eq 0 ] || fail "$command_name takes no nodes." ;;
    add-node|remove-node) [ "$#" -gt 0 ] || fail "$command_name requires at least one node or a selector." ;;
esac
# Reject ambiguous selectors and options before contacting Docker or loading config.
node_selector=''
explicit_nodes=no
for argument do
    case "$argument" in
        --all|--all-excluding-self|--workers)
            [ -z "$node_selector" ] && [ "$explicit_nodes" = no ] || fail 'Use exactly one selector without explicit nodes.'
            node_selector=$argument
            ;;
        -*) fail "Unknown option: $argument" ;;
        *)
            [ -z "$node_selector" ] || fail 'Use exactly one selector without explicit nodes.'
            case "$argument" in ''|*[!a-zA-Z0-9_.-]*) fail "Invalid node: $argument" ;; esac
            explicit_nodes=yes
            ;;
    esac
done
[ -z "$node_selector" ] || set --

# Only literal KEY=VALUE entries are accepted. No shell evaluation/expansion.
if [ "$command_name" != init ] && [ "$explicit_env_file" = yes ] && [ ! -f "$env_file" ]; then
    fail "Config not found: $env_file"
fi
if [ "$command_name" != init ] && [ -f "$env_file" ]; then
    line_number=0
    cr=$(printf '\r')
    while IFS= read -r line || [ -n "$line" ]; do
        line_number=$((line_number + 1))
        line=${line%"$cr"}
        case "$line" in ''|'#'*) continue ;; esac
        case "$line" in *=*) key=${line%%=*}; value=${line#*=} ;;
            *) fail "Invalid config entry at line $line_number." ;; esac
        case "$key" in
            KPL_STACK_NAME|KPL_CONTROL_NODE_ID|KPL_PEER_NETWORK|KPL_IMAGE|KPL_AGENT_CAPACITY|KPL_API_TOKEN|GRAFANA_ADMIN_PASSWORD|GRAFANA_ADMIN_USER|KPL_HTTP_PORT|PROMETHEUS_PORT|GRAFANA_PORT|KPL_MIN_AGENTS|KPL_DOCKER_TIMEOUT|KPL_IMAGE_PULL_TIMEOUT|KPL_CONTROLLER_STOP_TIMEOUT|KPL_AGENT_STOP_TIMEOUT) ;;
            *) fail "Unsupported config key at line $line_number." ;;
        esac
        case "$value" in \"*\") value=${value#\"}; value=${value%\"} ;;
            \'*\') value=${value#\'}; value=${value%\'} ;; esac
        if ! printenv "$key" >/dev/null 2>&1; then export "$key=$value"; fi
    done < "$env_file"
fi

KPL_STACK_NAME=${KPL_STACK_NAME:-kpl}
case "$KPL_STACK_NAME" in ''|[!a-z0-9]*|*[!a-z0-9_-]*) fail 'Invalid KPL_STACK_NAME; use lowercase letters, digits, hyphens or underscores.' ;; esac
KPL_PEER_NETWORK=${KPL_PEER_NETWORK:-$KPL_STACK_NAME-peers}
case "$KPL_PEER_NETWORK" in ''|-*|*[!a-zA-Z0-9_.-]*) fail 'Invalid KPL_PEER_NETWORK.' ;; esac
KPL_MIN_AGENTS=${KPL_MIN_AGENTS:-1}
docker_timeout=${KPL_DOCKER_TIMEOUT:-60}
image_pull_timeout=${KPL_IMAGE_PULL_TIMEOUT:-300}
controller_timeout=${KPL_CONTROLLER_STOP_TIMEOUT:-480}
agent_timeout=${KPL_AGENT_STOP_TIMEOUT:-240}
for number in "$docker_timeout" "$image_pull_timeout" "$controller_timeout" "$agent_timeout" "$KPL_MIN_AGENTS"; do
    case "$number" in ''|0*|*[!0-9]*) fail 'Timeouts and KPL_MIN_AGENTS must be positive integers without leading zeros.' ;; esac
    [ "$number" -gt 0 ] 2>/dev/null || fail 'Integer is out of range.'
done
export KPL_STACK_NAME KPL_PEER_NETWORK KPL_MIN_AGENTS
agent_label=kpl.$KPL_STACK_NAME.agent
application=kp2plab-v3
command -v docker >/dev/null 2>&1 || fail 'Docker CLI is required.'
command -v timeout >/dev/null 2>&1 || fail 'Linux timeout (coreutils) is required.'
command -v awk >/dev/null 2>&1 || fail 'awk is required.'
dock() { timeout -s TERM -k 5 "$docker_timeout" docker "$@"; }
[ "$(dock info --format '{{.Swarm.LocalNodeState}} {{.Swarm.ControlAvailable}}')" = 'active true' ] || fail 'Run against an active Swarm manager.'

if [ "$command_name" = init ]; then
    [ ! -e "$env_file" ] || fail "Config already exists: $env_file"
    control_node=${KPL_CONTROL_NODE_ID:-$(dock info --format '{{.Swarm.NodeID}}')}
    case "$control_node" in ''|*[!a-zA-Z0-9]*) fail 'Invalid control node ID.' ;; esac
    token=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
    password=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
    [ "${#token}" -eq 64 ] && [ "${#password}" -eq 64 ] || fail 'Could not generate credentials.'
    (umask 077; set -C; cat > "$env_file" <<EOF
# Literal KEY=VALUE entries; environment variables take precedence.
KPL_STACK_NAME=$KPL_STACK_NAME
KPL_CONTROL_NODE_ID=$control_node
KPL_PEER_NETWORK=$KPL_PEER_NETWORK
# Replace with a pushed image tag accessible from every node; digests also work.
# Each deploy resolves the tag to one digest without changing this file.
KPL_IMAGE=registry.example.com/kpl-v3:v3
# Tag pull timeout in seconds, separate from individual Docker command timeouts.
KPL_IMAGE_PULL_TIMEOUT=$image_pull_timeout
KPL_AGENT_CAPACITY=20
KPL_API_TOKEN=$token
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=$password
EOF
    ) || fail 'Could not create config (existing files are never overwritten).'
    printf 'Created %s (mode 0600). Set KPL_IMAGE and verify KPL_CONTROL_NODE_ID before deploy.\n' "$env_file"
    exit 0
fi

# Refuse to change an unrelated or unmarked stack with the same name.
services=$(dock service ls --quiet --filter "label=com.docker.stack.namespace=$KPL_STACK_NAME")
controller_present=no
agent_present=no
for service in $services; do
    identity=$(dock service inspect --format '{{.Spec.Name}}|{{index .Spec.Labels "io.kpl.application"}}' "$service")
    case "$identity" in
        "$KPL_STACK_NAME"_controller\|"$application"|"$KPL_STACK_NAME"_agent\|"$application"|"$KPL_STACK_NAME"_prometheus\|"$application"|"$KPL_STACK_NAME"_grafana\|"$application") ;;
        *) fail 'Existing stack contains an unrecognized service. See docs/swarm.md for migration; no changes made.' ;;
    esac
    case "$identity" in
        "${KPL_STACK_NAME}_controller|$application") controller_present=yes ;;
        "${KPL_STACK_NAME}_agent|$application") agent_present=yes ;;
    esac
done
service_exists() {
    case "$1" in
        "${KPL_STACK_NAME}_controller") [ "$controller_present" = yes ] ;;
        "${KPL_STACK_NAME}_agent") [ "$agent_present" = yes ] ;;
        *) return 1 ;;
    esac
}
resolve_nodes() {
    resolved_seen=' '
    for node do
        case "$node" in ''|-*|*[!a-zA-Z0-9_.-]*) fail "Invalid node: $node" ;; esac
        resolved_id=$(dock node inspect --format '{{.ID}}' "$node") || return 1
        case "$resolved_id" in ''|*[!a-zA-Z0-9]*) fail "Docker returned an invalid node ID for $node." ;; esac
        case "$resolved_seen" in *" $resolved_id "*) continue ;; esac
        resolved_seen="$resolved_seen$resolved_id "
        printf '%s\n' "$resolved_id"
    done
}
ready_node() {
    state=$(dock node inspect --format '{{.Description.Platform.OS}} {{.Status.State}} {{.Spec.Availability}}' "$1")
    [ "$state" = 'linux ready active' ] || fail "Node $1 must be Linux, Ready and Active; got $state."
}
selected_nodes() { dock node ls --quiet --filter "node.label=$agent_label=true"; }
command_nodes() {
    if [ -z "$node_selector" ]; then resolve_nodes "$@"; return; fi
    if [ "$command_name" = remove-node ]; then
        candidates=$(selected_nodes) || fail 'Cannot list Agent nodes for this stack; no changes made.'
    else
        candidates=$(dock node ls --quiet) || fail 'Cannot list Swarm nodes; no changes made.'
    fi
    selector_self=''
    if [ "$node_selector" = --all-excluding-self ]; then
        selector_self=$(dock info --format '{{.Swarm.NodeID}}') || fail 'Cannot identify the Swarm node of the current Docker daemon.'
        case "$selector_self" in ''|*[!a-zA-Z0-9]*) fail 'Docker returned an invalid current Swarm node ID.' ;; esac
    fi
    selection=''
    selection_seen=' '
    for candidate in $candidates; do
        case "$candidate" in ''|*[!a-zA-Z0-9]*) fail 'Docker returned an invalid node list.' ;; esac
        selection_record=$(dock node inspect --format '{{.ID}}|{{.Spec.Role}}|{{.Description.Platform.OS}}|{{.Status.State}}|{{.Spec.Availability}}' "$candidate") || fail "Cannot inspect node $candidate; no changes made."
        selection_id=${selection_record%%|*}; selection_rest=${selection_record#*|}
        selection_role=${selection_rest%%|*}; selection_rest=${selection_rest#*|}
        selection_os=${selection_rest%%|*}; selection_rest=${selection_rest#*|}
        selection_state=${selection_rest%%|*}; selection_availability=${selection_rest#*|}
        case "$selection_id" in ''|*[!a-zA-Z0-9]*) fail "Docker returned an invalid node ID for $candidate." ;; esac
        case "$selection_role" in manager|worker) ;; *) fail "Docker returned an invalid role for node $selection_id." ;; esac
        for field in "$selection_os" "$selection_state" "$selection_availability"; do
            case "$field" in ''|*[!a-zA-Z0-9_.-]*) fail "Docker returned invalid node metadata for $selection_id." ;; esac
        done
        # Require the complete five-field record even if missing delimiters would
        # otherwise leave plausible values in the shell parameter expansions.
        [ "$selection_record" = "$selection_id|$selection_role|$selection_os|$selection_state|$selection_availability" ] || fail "Docker returned incomplete node metadata for $selection_id."
        case "$selection_seen" in *" $selection_id "*) continue ;; esac
        selection_seen="$selection_seen$selection_id "
        [ "$selection_id" != "$selector_self" ] || continue
        if [ "$node_selector" = --workers ] && [ "$selection_role" != worker ]; then continue; fi
        # Removal must retain unavailable labeled nodes so the strict preflight
        # refuses cleanup that cannot be verified, before changing any placement.
        if [ "$command_name" != remove-node ] && [ "$selection_os $selection_state $selection_availability" != 'linux ready active' ]; then
            printf 'Skipping node %s: requires Linux, Ready and Active; got %s %s %s.\n' "$selection_id" "$selection_os" "$selection_state" "$selection_availability" >&2
            continue
        fi
        selection="$selection $selection_id"
    done
    [ -n "$selection" ] || fail "Selector $node_selector matched no eligible nodes."
    for selection_id in $selection; do printf '%s\n' "$selection_id"; done
}
report_nodes() {
    [ -n "$1" ] || return 0
    set -- $1
    printf 'Selected %s node(s): %s\n' "$#" "$*" >&2
}
resolve_deployment_image() {
    case "$KPL_IMAGE" in
        *@sha256:*) export KPL_IMAGE; return 0 ;;
    esac
    requested_image=$KPL_IMAGE
    printf 'Resolving image tag %s (pull timeout: %ss)...\n' "$requested_image" "$image_pull_timeout"
    # Pull through the selected daemon to retain its registry TLS/HTTP policy.
    # Use that daemon's native platform regardless of the caller's default;
    # Docker still reports the top-level manifest/index digest for this pull.
    if ! pull_output=$(
        unset DOCKER_DEFAULT_PLATFORM
        timeout -s TERM -k 5 "$image_pull_timeout" docker pull "$requested_image"
    ); then
        [ -z "$pull_output" ] || printf '%s\n' "$pull_output" >&2
        fail 'Image pull failed; no deployment changes made.'
    fi
    # Do not fall back to local RepoDigests: those can contain stale or multiple
    # references. A successful pull must identify exactly one registry digest.
    resolved_digest=$(printf '%s\n' "$pull_output" | awk '
        { sub(/\r$/, "") }
        /^Digest:/ {
            count++
            if (NF != 2 || $0 != "Digest: " $2 || $2 !~ /^sha256:[a-f0-9]+$/ || length($2) != 71) invalid=1
            digest=$2
        }
        END {
            if (count != 1 || invalid) exit 1
            print digest
        }
    ') || fail 'Cannot resolve exactly one sha256 digest from the successful pull; no deployment changes made.'
    # Static validation already required a tag on the final path component.
    KPL_IMAGE=${requested_image%:*}@$resolved_digest
    export KPL_IMAGE
    sh "$root/scripts/check-swarm.sh" --config-only
    printf 'Deploying resolved image %s\n' "$KPL_IMAGE"
}
placement_exclusions() {
    action=$1
    excluded_nodes=$2
    [ -n "$excluded_nodes" ] || return 0
    set -- service update --detach=true --no-resolve-image
    for node in $excluded_nodes; do
        set -- "$@" "--constraint-$action" "node.id!=$node"
    done
    dock "$@" "${KPL_STACK_NAME}_agent"
}

task_format='{{.NodeID}}|{{.Status.State}}|{{if .Status.ContainerStatus}}{{.Status.ContainerStatus.ContainerID}}|{{.Status.ContainerStatus.PID}}|{{.Status.ContainerStatus.ExitCode}}{{else}}-|0|-{{end}}|{{if .Status.Err}}error{{else}}ok{{end}}'
# Keep active tasks, the latest task per node, and every retained failure.
# Docker sorts each replicated slot / global node newest first. A later task
# canceled before process startup cannot prove that an earlier failure recovered.
capture_tasks() {
    task_service=$1
    task_node=${2:-}
    if [ -n "$task_node" ]; then
        task_ids=$(dock service ps --quiet --no-trunc --filter "node=$task_node" "$task_service") || return 1
    else
        task_ids=$(dock service ps --quiet --no-trunc "$task_service") || return 1
    fi
    [ -n "$task_ids" ] || fail "No task history for $task_service ${task_node:-}; cleanup cannot be verified automatically."
    seen=' '
    for task_id in $task_ids; do
        record=$(dock inspect --type task --format "$task_format" "$task_id") || return 1
        node=${record%%|*}; rest=${record#*|}; state=${rest%%|*}
        # Pausing a replicated service preserves its slot and shutdown history,
        # with a replacement that cannot be assigned. It has never run a process;
        # resuming the service can cancel that unassigned replacement as Shutdown.
        if [ -z "$node" ]; then
            case "$record" in
                '|new|-|0|-|'*|'|pending|-|0|-|'*|'|allocated|-|0|-|'*|'|shutdown|-|0|-|'*) continue ;;
            esac
            fail "Task $task_id has no verifiable node/container assignment."
        fi
        include=yes
        case "$state" in complete|shutdown)
            case "$record" in *'|0|0|ok')
                case "$seen" in *" $node "*) include=no ;; esac ;;
            esac ;;
        esac
        seen="$seen$node "
        if [ "$include" = yes ]; then
            # Readiness is needed to trust a remote container stop acknowledgment.
            node_state=$(dock node inspect --format '{{.Description.Platform.OS}} {{.Status.State}}' "$node") || return 1
            [ "$node_state" = 'linux ready' ] || fail "Task $task_id is on unavailable node $node; cleanup cannot be verified."
            printf '%s\n' "$task_id"
        fi
    done
}
wait_tasks() {
    wait_ids=$1
    deadline=$(( $(date +%s) + $2 ))
    for task_id in $wait_ids; do
        while :; do
            record=$(dock inspect --type task --format "$task_format" "$task_id") || fail "Cannot inspect task $task_id; cleanup is unverified."
            task_node=${record%%|*}; rest=${record#*|}
            state=${rest%%|*}; rest=${rest#*|}
            container=${rest%%|*}; rest=${rest#*|}
            pid=${rest%%|*}; rest=${rest#*|}
            exit_code=${rest%%|*}; task_error=${rest#*|}
            node_state=$(dock node inspect --format '{{.Status.State}}' "$task_node")
            [ "$node_state" = ready ] || fail "Node $task_node became unavailable; task $task_id cleanup is unverified."
            case "$state" in
                complete|shutdown)
                    [ -n "$container" ] && [ "$container" != - ] && [ "$pid" = 0 ] && [ "$exit_code" = 0 ] && [ "$task_error" = ok ] || fail "Task $task_id stopped without a clean container exit (state=$state, exit=$exit_code). Inspect it before retrying."
                    break ;;
                failed|rejected|orphaned|remove) fail "Task $task_id is $state; cleanup is unverified. Inspect it before retrying." ;;
            esac
            [ "$(date +%s)" -lt "$deadline" ] || fail "Timed out waiting for task $task_id ($state); stack retained for inspection."
            sleep 2
        done
    done
}

case "$command_name" in
    status)
        dock stack services "$KPL_STACK_NAME"
        if service_exists "${KPL_STACK_NAME}_agent"; then dock service ps --no-trunc "${KPL_STACK_NAME}_agent"; fi
        printf '\nAgent placement label: %s=true\n' "$agent_label"
        dock node ls --filter "node.label=$agent_label=true"
        ;;
    deploy)
        : "${KPL_IMAGE:?Set KPL_IMAGE in .env.swarm or environment}"
        : "${KPL_CONTROL_NODE_ID:?Set KPL_CONTROL_NODE_ID}"
        : "${KPL_API_TOKEN:?Set KPL_API_TOKEN}"
        : "${GRAFANA_ADMIN_PASSWORD:?Set GRAFANA_ADMIN_PASSWORD}"
        sh "$root/scripts/check-swarm.sh" --config-only
        # Validate interpolation without printing credentials.
        dock stack config --compose-file "$root/stack.swarm.yaml" >/dev/null
        ready_node "$KPL_CONTROL_NODE_ID"
        nodes=$(command_nodes "$@")
        for node in $nodes; do ready_node "$node"; done
        report_nodes "$nodes"
        resolve_deployment_image
        networks=$(dock network ls --quiet --filter "name=^$KPL_PEER_NETWORK$")
        if [ -z "$networks" ]; then
            dock network create --driver overlay --attachable --label "io.kpl.application=$application" --label "io.kpl.stack=$KPL_STACK_NAME" "$KPL_PEER_NETWORK"
        else
            [ "$(dock network inspect --format '{{.Name}} {{.Driver}} {{.Attachable}}' "$KPL_PEER_NETWORK")" = "$KPL_PEER_NETWORK overlay true" ] || fail 'Peer network must be an attachable overlay.'
        fi
        for node in $nodes; do dock node update --label-add "$agent_label=true" "$node"; done
        timeout -s TERM -k 5 "$docker_timeout" sh "$root/scripts/check-swarm.sh"
        dock stack deploy --with-registry-auth --resolve-image always --detach=true --compose-file "$root/stack.swarm.yaml" "$KPL_STACK_NAME"
        printf 'Deployment submitted. Use status, then check registered Agents in the Controller before running an experiment.\n'
        ;;
    add-node)
        service_exists "${KPL_STACK_NAME}_agent" || fail 'Deploy the stack before adding nodes.'
        nodes=$(command_nodes "$@")
        for node in $nodes; do ready_node "$node"; done
        report_nodes "$nodes"
        for node in $nodes; do dock node update --label-add "$agent_label=true" "$node"; done
        # Resume a node left excluded by an interrupted removal.
        placement_exclusions rm "$nodes"
        printf 'Agent placement updated. Use status and wait for Controller registration.\n'
        ;;
    remove-node)
        service_exists "${KPL_STACK_NAME}_agent" || fail 'Agent service does not exist.'
        nodes=$(command_nodes "$@")
        tasks=''
        for node in $nodes; do
            ready_node "$node"
            captured=$(capture_tasks "${KPL_STACK_NAME}_agent" "$node")
            tasks="$tasks $captured"
        done
        report_nodes "$nodes"
        # Changing a node label first invalidates the old task's own constraints:
        # Docker can report Rejected with stale PID/exit data even after exit 0.
        # A service-only exclusion preserves the old task's valid node labels
        # while the orchestrator requests an ordinary, verifiable shutdown.
        placement_exclusions add "$nodes"
        wait_tasks "$tasks" "$agent_timeout"
        # Catch a scheduler replacement racing with the placement update.
        for node in $nodes; do
            remaining=$(capture_tasks "${KPL_STACK_NAME}_agent" "$node")
            wait_tasks "$remaining" "$agent_timeout"
        done
        for node in $nodes; do dock node update --label-rm "$agent_label" "$node"; done
        placement_exclusions rm "$nodes"
        printf 'Selected Agents stopped with clean exits. Other node labels were unchanged.\n'
        ;;
    remove)
        if [ -z "$services" ]; then
            printf 'Stack %s has no services; no changes made.\n' "$KPL_STACK_NAME"
            exit 0
        fi
        controller_tasks=''; agent_tasks=''
        if service_exists "${KPL_STACK_NAME}_controller"; then controller_tasks=$(capture_tasks "${KPL_STACK_NAME}_controller"); fi
        if service_exists "${KPL_STACK_NAME}_agent"; then agent_tasks=$(capture_tasks "${KPL_STACK_NAME}_agent"); fi
        nodes=$(selected_nodes)
        # All task/node checks above finish before the first destructive action.
        if service_exists "${KPL_STACK_NAME}_controller"; then
            # Scaling to zero destroys the replicated slot and may immediately
            # garbage-collect its shutdown evidence. Contradictory role
            # constraints keep the slot, but prevent replacement on any node.
            dock service update --detach=true --no-resolve-image --constraint-add 'node.role==manager' --constraint-add 'node.role==worker' "${KPL_STACK_NAME}_controller"
            wait_tasks "$controller_tasks" "$controller_timeout"
            remaining=$(capture_tasks "${KPL_STACK_NAME}_controller")
            wait_tasks "$remaining" "$controller_timeout"
        fi
        if service_exists "${KPL_STACK_NAME}_agent"; then placement_exclusions add "$nodes"; fi
        wait_tasks "$agent_tasks" "$agent_timeout"
        if service_exists "${KPL_STACK_NAME}_agent"; then
            remaining=$(capture_tasks "${KPL_STACK_NAME}_agent")
            wait_tasks "$remaining" "$agent_timeout"
        fi
        for node in $nodes; do dock node update --label-rm "$agent_label" "$node"; done
        dock stack rm "$KPL_STACK_NAME"
        deadline=$(( $(date +%s) + $docker_timeout ))
        while :; do
            remaining_services=$(dock service ls --quiet --filter "label=com.docker.stack.namespace=$KPL_STACK_NAME") || fail 'Cannot verify stack removal; Docker service lookup failed.'
            [ -n "$remaining_services" ] || break
            [ "$(date +%s)" -lt "$deadline" ] || fail 'Stack removal is still pending; inspect status.'
            sleep 2
        done
        printf 'Stack services removed after Controller/Agent clean exits. Data volumes and Peer network preserved.\n'
        ;;
esac
