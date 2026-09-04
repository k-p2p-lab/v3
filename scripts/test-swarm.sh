#!/bin/sh
# Stateful fake Docker tests. No daemon socket, registry or cluster is accessed.
set -eu
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
scratch=$(mktemp -d)
trap 'result=$?; if [ "$result" -ne 0 ]; then printf "Manager test case %s failed\n" "${case_number:-setup}" >&2; if [ -n "${KPL_TEST_STATE:-}" ]; then cat "$KPL_TEST_STATE/output" "$KPL_TEST_STATE/events" "$KPL_TEST_STATE/calls" >&2; fi; fi; rm -rf "$scratch"; exit "$result"' EXIT
mkdir "$scratch/bin"
: > "$scratch/config.env"
KPL_TEST_REAL_TIMEOUT=$(command -v timeout)
export KPL_TEST_REAL_TIMEOUT
cat > "$scratch/bin/timeout" <<'MOCK_TIMEOUT'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$KPL_TEST_STATE/timeouts"
exec "$KPL_TEST_REAL_TIMEOUT" "$@"
MOCK_TIMEOUT
chmod +x "$scratch/bin/timeout"

cat > "$scratch/bin/docker" <<'MOCK_DOCKER'
#!/bin/sh
set -eu
s=$KPL_TEST_STATE
stack=${KPL_STACK_NAME:-kpl}
printf '%s\n' "$*" >> "$s/calls"
die() { printf 'Unexpected Docker call: %s\n' "$*" >&2; exit 97; }
event() { printf '%s\n' "$1" >> "$s/events"; }
for last do :; done
case "$1 $2" in
    'pull '*)
        [ "$#" = 2 ] || die "$@"
        # The selected platform must not collapse a multi-platform tag to the
        # Manager's local platform. Do not consult local image IDs/RepoDigests.
        [ "${DOCKER_DEFAULT_PLATFORM+x}" != x ] || die 'DOCKER_DEFAULT_PLATFORM reached pull'
        digest=${KPL_TEST_PULL_DIGEST:-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
        printf 'Pulling from %s\n' "$2"
        case "${KPL_TEST_PULL_FAULT:-none}" in
            missing) printf 'Status: Image is up to date\n' ;;
            malformed) printf 'Digest: sha256:abcd\n' ;;
            uppercase) printf 'Digest: sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n' ;;
            multiple) printf 'Digest: sha256:%s\nDigest: sha256:%s\n' "$digest" "$digest" ;;
            mixed) printf 'Digest: sha256:%s\nDigest: malformed\n' "$digest" ;;
            trailing) printf 'Digest: sha256:%s trailing-data\n' "$digest" ;;
            fail) printf 'Digest: sha256:%s\n' "$digest"; printf 'mock registry unavailable\n' >&2; exit 1 ;;
            none) printf 'Digest: sha256:%s\n' "$digest" ;;
            *) die "${KPL_TEST_PULL_FAULT}" ;;
        esac ;;
    'info --format')
        case "$3" in
            '{{.Swarm.NodeID}}') printf 'control1\n' ;;
            '{{.Swarm.LocalNodeState}} {{.Swarm.ControlAvailable}}') printf 'active true\n' ;;
            *) die "$@" ;;
        esac ;;
    'service ls')
        case "$*" in *"label=com.docker.stack.namespace=$stack"*) ;; *) die "$@" ;; esac
        if [ -f "$s/removed" ]; then
            if [ "${KPL_TEST_FINAL_LIST_FAIL:-0}" = 1 ]; then printf 'mock final service listing failed\n' >&2; exit 2; fi
            exit 0
        fi
        [ "${KPL_TEST_EMPTY_STACK:-0}" = 0 ] || exit 0
        case "$*" in
            *"name=${stack}_controller") printf 'svccontroller\n' ;;
            *"name=${stack}_agent") printf 'svcagent\n' ;;
            *name=*) die "$@" ;;
            *) printf 'svccontroller\nsvcagent\nsvcprometheus\nsvcgrafana\n' ;;
        esac ;;
    'service inspect')
        if [ "${KPL_TEST_INSPECT_FAIL:-0}" = 1 ]; then printf 'mock service inspection failed\n' >&2; exit 2; fi
        case "$last" in
            svccontroller|"${stack}_controller") name=${stack}_controller ;;
            svcagent|"${stack}_agent") name=${stack}_agent ;;
            svcprometheus) name=${stack}_prometheus ;;
            svcgrafana) name=${stack}_grafana ;;
            *) die "$@" ;;
        esac
        case "$4" in
            *io.kpl.application*)
                owner=kp2plab-v3
                if [ "${KPL_TEST_FOREIGN:-0}" = 1 ]; then owner=another-application; fi
                if [ "${KPL_TEST_FOREIGN_STACK:-0}" = 1 ]; then name=another_stack_agent; fi
                printf '%s|%s\n' "$name" "$owner" ;;
            '{{.Spec.Name}}') printf '%s\n' "$name" ;;
            *) die "$@" ;;
        esac ;;
    'service ps')
        [ "${KPL_TEST_EMPTY_HISTORY:-0}" = 0 ] || exit 0
        case "$last" in
            "${stack}_controller")
                # Pausing placement preserves the running slot's old task and
                # introduces an unassigned replacement ahead of it in history.
                if [ -f "$s/controller-stopped" ]; then printf 'pendingC\n'; fi
                printf 'taskC\n' ;;
            "${stack}_agent")
                case "$*" in
                    *node=worker1*) printf 'taskA1\noldA1\n' ;;
                    *node=worker2*) printf 'taskA2\n' ;;
                    *) printf 'taskA1\noldA1\ntaskA2\n' ;;
                esac ;;
            *) die "$@" ;;
        esac ;;
    'inspect --type')
        [ "$3" = task ] || die "$@"
        state=running; pid=42; code=0; issue=ok
        case "$last" in
            pendingC)
                [ -f "$s/controller-stopped" ] || die "$@"
                node=''; state=${KPL_TEST_PENDING_STATE:-pending}
                container=${KPL_TEST_PENDING_CONTAINER:--}; pid=0; code=-; issue=error ;;
            taskC)
                node=control1; container=containerC
                if [ -f "$s/controller-stopped" ]; then
                    state=shutdown; pid=0
                    case "${KPL_TEST_CONTROLLER_STOP:-clean}" in
                        failed) state=failed; code=1; issue=error ;;
                        nonzero) code=1 ;;
                    esac
                fi ;;
            taskA1|taskA2)
                if [ "$last" = taskA1 ]; then node=worker1; else node=worker2; fi
                container=container$node
                if [ -f "$s/stopped-$node" ]; then
                    state=shutdown; pid=0
                    if [ "${KPL_TEST_AGENT_STOP:-clean}" = failed ]; then state=failed; code=1; issue=error; fi
                fi
                # Swarm can reject an existing task when its placement label
                # disappears before clean shutdown has been verified.
                if [ -f "$s/rejected-$node" ]; then state=rejected; pid=42; issue=error; fi ;;
            oldA1)
                node=worker1; container=oldContainer; state=shutdown; pid=0
                if [ "${KPL_TEST_OLD_FAILED:-0}" = 1 ]; then state=failed; code=1; issue=error; fi ;;
            *) die "$@" ;;
        esac
        if [ "$last" != oldA1 ] && [ "$state" = shutdown ] && [ "$code" = 0 ] && [ ! -f "$s/clean-$last" ]; then
            event "clean-$last"
            : > "$s/clean-$last"
        fi
        printf '%s|%s|%s|%s|%s|%s\n' "$node" "$state" "$container" "$pid" "$code" "$issue" ;;
    'node inspect')
        format=$4; shift 4
        for node do
            case "$node" in
                worker-a) node=worker1 ;;
                worker-b) node=worker2 ;;
                control1|worker1|worker2) ;;
                *) die "$@" ;;
            esac
            state=ready
            if [ "${KPL_TEST_DOWN_NODE:-}" = "$node" ]; then state=down; fi
            case "$format" in
                '{{.ID}}') printf '%s\n' "$node" ;;
                '{{.Status.State}}') printf '%s\n' "$state" ;;
                '{{.Description.Platform.OS}} {{.Status.State}}') printf 'linux %s\n' "$state" ;;
                '{{.Description.Platform.OS}} {{.Status.State}} {{.Spec.Availability}}') printf 'linux %s active\n' "$state" ;;
                '{{.ID}} {{.Description.Platform.OS}} {{.Status.State}} {{.Spec.Availability}}') printf '%s linux %s active\n' "$node" "$state" ;;
                *) die "$format" ;;
            esac
        done ;;
    'node ls')
        case "$*" in *"node.label=kpl.$stack.agent=true") ;; *) die "$@" ;; esac
        for node in worker1 worker2; do [ -f "$s/unlabeled-$node" ] || printf '%s\n' "$node"; done ;;
    'node update')
        case "$3 $4" in
            "--label-rm kpl.$stack.agent")
                case "$last" in worker1) task=taskA1 ;; worker2) task=taskA2 ;; *) die "$@" ;; esac
                if [ ! -f "$s/clean-$task" ]; then : > "$s/rejected-$last"; fi
                : > "$s/unlabeled-$last"; event "unlabel-$last" ;;
            "--label-add kpl.$stack.agent=true")
                rm -f "$s/unlabeled-$last"; event "label-$last" ;;
            *) die "$@" ;;
        esac ;;
    'service update')
        [ "$3 $4" = '--detach=true --no-resolve-image' ] || die "$@"
        if [ "$last" = "${stack}_controller" ]; then
            [ "$#" = 9 ] && [ "$5 $6 $7 $8" = '--constraint-add node.role==manager --constraint-add node.role==worker' ] || die "$@"
            event pause-controller; : > "$s/controller-stopped"
            exit 0
        fi
        [ "$last" = "${stack}_agent" ] || die "$@"
        shift 4
        while [ "$#" -gt 1 ]; do
            action=$1; constraint=$2
            case "$constraint" in 'node.id!=worker1'|'node.id!=worker2') node=${constraint#node.id!=} ;; *) die "$@" ;; esac
            case "$action" in
                --constraint-add)
                    : > "$s/excluded-$node"; : > "$s/stopped-$node"; event "exclude-$node" ;;
                --constraint-rm)
                    rm -f "$s/excluded-$node"; event "include-$node" ;;
                *) die "$@" ;;
            esac
            shift 2
        done ;;
    'network ls')
        [ "${KPL_TEST_NO_NETWORK:-0}" = 1 ] || printf 'network1\n' ;;
    'network inspect') printf '%s overlay true\n' "$KPL_PEER_NETWORK" ;;
    'network create') event network-create; printf 'network1\n' ;;
    'stack config')
        printf '%s' "${KPL_API_TOKEN:-}" > "$s/config-token"
        printf '%s\n' "${KPL_IMAGE:-}" >> "$s/config-images" ;;
    'stack deploy')
        printf '%s\n' "${KPL_IMAGE:-}" >> "$s/deploy-images"
        event stack-deploy ;;
    'stack services') printf 'mock stack services\n' ;;
    'stack rm')
        [ "$3" = "$stack" ] || die "$@"
        event stack-rm; : > "$s/removed" ;;
    *) die "$@" ;;
esac
MOCK_DOCKER
chmod +x "$scratch/bin/docker"
export PATH="$scratch/bin:$PATH"

case_number=0
reset_case() {
    case_number=$((case_number + 1))
    KPL_TEST_STATE=$scratch/case-$case_number
    export KPL_TEST_STATE
    mkdir "$KPL_TEST_STATE"
    : > "$KPL_TEST_STATE/calls"
    : > "$KPL_TEST_STATE/events"
    : > "$KPL_TEST_STATE/timeouts"
    export KPL_STACK_NAME=lab KPL_PEER_NETWORK=lab-peers KPL_CONTROL_NODE_ID=control1
    export KPL_IMAGE=registry.example/kpl@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
    export KPL_API_TOKEN=private-test-token GRAFANA_ADMIN_PASSWORD=private-test-password
    export KPL_DOCKER_TIMEOUT=3 KPL_CONTROLLER_STOP_TIMEOUT=3 KPL_AGENT_STOP_TIMEOUT=3
    unset KPL_TEST_FOREIGN KPL_TEST_FOREIGN_STACK KPL_TEST_DOWN_NODE KPL_TEST_CONTROLLER_STOP KPL_TEST_AGENT_STOP KPL_TEST_EMPTY_STACK KPL_TEST_NO_NETWORK KPL_MIN_AGENTS KPL_TEST_FINAL_LIST_FAIL KPL_TEST_INSPECT_FAIL KPL_TEST_EMPTY_HISTORY KPL_TEST_OLD_FAILED KPL_TEST_PENDING_STATE KPL_TEST_PENDING_CONTAINER KPL_TEST_PULL_DIGEST KPL_TEST_PULL_FAULT KPL_IMAGE_PULL_TIMEOUT DOCKER_DEFAULT_PLATFORM
}
run() { sh "$root/scripts/swarm.sh" --env-file "$scratch/config.env" "$@" > "$KPL_TEST_STATE/output" 2>&1; }
reject() {
    if run "$@"; then
        printf 'Unexpected acceptance of %s\n' "$*" >&2
        cat "$KPL_TEST_STATE/output" >&2
        exit 1
    fi
}
no_stack_removal() { ! grep -q '^stack-rm$' "$KPL_TEST_STATE/events"; }
no_mutation() { [ ! -s "$KPL_TEST_STATE/events" ]; }

reset_case
run remove
cat > "$scratch/expected" <<'EXPECTED'
pause-controller
clean-taskC
exclude-worker1
exclude-worker2
clean-taskA1
clean-taskA2
unlabel-worker1
unlabel-worker2
stack-rm
EXPECTED
cmp "$scratch/expected" "$KPL_TEST_STATE/events"
# The pending task is inspected by capture_tasks, but never waited on or treated
# as evidence that the previous Controller exited. The clean-taskC event above
# must occur before any Agent exclusion.
[ "$(grep -c 'inspect --type task .* pendingC$' "$KPL_TEST_STATE/calls")" = 1 ]
grep -q 'Data volumes and Peer network preserved' "$KPL_TEST_STATE/output"

for pending_state in new allocated shutdown; do
    reset_case
    export KPL_TEST_PENDING_STATE=$pending_state
    run remove
    cmp "$scratch/expected" "$KPL_TEST_STATE/events"
done

# Only never-assigned new/pending/allocated/shutdown tasks are safe to skip. An unknown
# process assignment or state must prevent proceeding to Agent shutdown.
reset_case
export KPL_TEST_PENDING_CONTAINER=unexpected-container
reject remove
no_stack_removal
grep -q 'pendingC has no verifiable node/container assignment' "$KPL_TEST_STATE/output"
if grep -q '^exclude-\|^unlabel-' "$KPL_TEST_STATE/events"; then exit 1; fi

reset_case
export KPL_TEST_PENDING_STATE=running
reject remove
no_stack_removal
grep -q 'pendingC has no verifiable node/container assignment' "$KPL_TEST_STATE/output"
if grep -q '^exclude-\|^unlabel-' "$KPL_TEST_STATE/events"; then exit 1; fi

reset_case
run remove-node worker-a
printf 'exclude-worker1\nclean-taskA1\nunlabel-worker1\ninclude-worker1\n' > "$scratch/expected"
cmp "$scratch/expected" "$KPL_TEST_STATE/events"
[ ! -e "$KPL_TEST_STATE/excluded-worker1" ]
[ ! -e "$KPL_TEST_STATE/stopped-worker2" ]
[ ! -e "$KPL_TEST_STATE/unlabeled-worker2" ]
grep -q 'node update --label-rm kpl.lab.agent worker1' "$KPL_TEST_STATE/calls"
if grep -q 'label-rm kpl.agent\|label-rm kpl.other.agent' "$KPL_TEST_STATE/calls"; then exit 1; fi

# The fake must reproduce Swarm's stale-PID rejection for the unsafe old order,
# so moving label removal ahead of verified shutdown cannot pass these tests.
reset_case
docker node update --label-rm kpl.lab.agent worker1 > "$KPL_TEST_STATE/output"
[ "$(docker inspect --type task --format unused taskA1)" = 'worker1|rejected|containerworker1|42|0|error' ]
reject remove-node worker-a
no_stack_removal
grep -q 'cleanup is unverified' "$KPL_TEST_STATE/output"
[ -e "$KPL_TEST_STATE/excluded-worker1" ]
if grep -q '^include-worker1$' "$KPL_TEST_STATE/events"; then exit 1; fi

reset_case
export KPL_TEST_FOREIGN=1
reject remove
no_mutation
grep -q 'unrecognized service' "$KPL_TEST_STATE/output"

reset_case
export KPL_TEST_FOREIGN_STACK=1
reject remove
no_mutation
grep -q 'unrecognized service' "$KPL_TEST_STATE/output"

reset_case
export KPL_TEST_DOWN_NODE=worker2
reject remove
no_mutation
grep -q 'cleanup cannot be verified' "$KPL_TEST_STATE/output"

reset_case
export KPL_TEST_EMPTY_HISTORY=1
reject remove
no_mutation
grep -q 'No task history' "$KPL_TEST_STATE/output"

reset_case
export KPL_TEST_INSPECT_FAIL=1
reject remove
no_mutation
grep -q 'mock service inspection failed' "$KPL_TEST_STATE/output"

reset_case
export KPL_TEST_FINAL_LIST_FAIL=1
reject remove
grep -q '^stack-rm$' "$KPL_TEST_STATE/events"
grep -q 'mock final service listing failed' "$KPL_TEST_STATE/output"
if grep -q 'Stack services removed after' "$KPL_TEST_STATE/output"; then exit 1; fi

reset_case
export KPL_TEST_AGENT_STOP=failed
reject remove
no_stack_removal
grep -q 'cleanup is unverified' "$KPL_TEST_STATE/output"
# Failed shutdown retains both placement labels and exclusions. Retrying must
# still inspect that failed task and must not re-enable the excluded Agents.
for node in worker1 worker2; do
    [ -e "$KPL_TEST_STATE/excluded-$node" ]
    [ ! -e "$KPL_TEST_STATE/unlabeled-$node" ]
done
reject remove
no_stack_removal
grep -q 'cleanup is unverified' "$KPL_TEST_STATE/output"
if grep -q '^unlabel-\|^include-' "$KPL_TEST_STATE/events"; then exit 1; fi

reset_case
export KPL_TEST_AGENT_STOP=failed
reject remove-node worker-a
[ -e "$KPL_TEST_STATE/excluded-worker1" ]
[ ! -e "$KPL_TEST_STATE/unlabeled-worker1" ]
reject remove-node worker-a
grep -q 'cleanup is unverified' "$KPL_TEST_STATE/output"
if grep -q '^unlabel-\|^include-' "$KPL_TEST_STATE/events"; then exit 1; fi

reset_case
export KPL_TEST_OLD_FAILED=1
reject remove
no_stack_removal
# A later clean task cannot prove that an older failed Agent cleaned its peers.
grep -q 'oldA1' "$KPL_TEST_STATE/output"
reject remove
no_stack_removal
grep -q 'oldA1' "$KPL_TEST_STATE/output"

reset_case
export KPL_TEST_CONTROLLER_STOP=nonzero
reject remove
no_stack_removal
if grep -q '^exclude-\|^unlabel-' "$KPL_TEST_STATE/events"; then exit 1; fi
grep -q 'without a clean container exit' "$KPL_TEST_STATE/output"

reset_case
: > "$KPL_TEST_STATE/excluded-worker1"
: > "$KPL_TEST_STATE/unlabeled-worker1"
run add-node worker-a
printf 'label-worker1\ninclude-worker1\n' > "$scratch/expected"
cmp "$scratch/expected" "$KPL_TEST_STATE/events"
[ ! -e "$KPL_TEST_STATE/excluded-worker1" ]
[ ! -e "$KPL_TEST_STATE/unlabeled-worker1" ]

reset_case
run deploy worker-a
printf 'label-worker1\nstack-deploy\n' > "$scratch/expected"
cmp "$scratch/expected" "$KPL_TEST_STATE/events"
[ "$(cat "$KPL_TEST_STATE/deploy-images")" = "$KPL_IMAGE" ]
if grep -q '^pull ' "$KPL_TEST_STATE/calls"; then exit 1; fi

reset_case
export KPL_IMAGE=registry.example/kpl@sha256:abcd KPL_TEST_NO_NETWORK=1
reject deploy worker-a
no_mutation
grep -q 'sha256' "$KPL_TEST_STATE/output"
if grep -q '^pull ' "$KPL_TEST_STATE/calls"; then exit 1; fi

# Explicit digests, including tag@digest references, are already immutable.
reset_case
export KPL_IMAGE=registry.example:5000/team/kpl:v3@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
run deploy worker-a
[ "$(cat "$KPL_TEST_STATE/deploy-images")" = "$KPL_IMAGE" ]
if grep -q '^pull \|^image inspect\|^inspect --format' "$KPL_TEST_STATE/calls"; then exit 1; fi

# Reusing the same mutable tag must resolve anew on every deploy. Registry
# ports and nested repository paths survive tag stripping, and config bytes
# remain unchanged. Deliberately set a platform override that pull must unset.
reset_case
unset KPL_IMAGE
tag=registry.example:5000/team/kpl:v3
printf 'KPL_IMAGE=%s\n' "$tag" > "$scratch/tag.env"
cp "$scratch/tag.env" "$scratch/tag-original.env"
export DOCKER_DEFAULT_PLATFORM=linux/arm64
export KPL_TEST_PULL_DIGEST=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
sh "$root/scripts/swarm.sh" --env-file "$scratch/tag.env" deploy worker-a > "$KPL_TEST_STATE/output" 2>&1
grep -Fxq -- "-s TERM -k 5 300 docker pull $tag" "$KPL_TEST_STATE/timeouts"
export KPL_TEST_PULL_DIGEST=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
sh "$root/scripts/swarm.sh" --env-file "$scratch/tag.env" deploy worker-a > "$KPL_TEST_STATE/output" 2>&1
printf 'registry.example:5000/team/kpl@sha256:%s\nregistry.example:5000/team/kpl@sha256:%s\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb > "$scratch/expected"
cmp "$scratch/expected" "$KPL_TEST_STATE/deploy-images"
cmp "$scratch/tag-original.env" "$scratch/tag.env"
[ "$(grep -Fc "pull $tag" "$KPL_TEST_STATE/calls")" = 2 ]
if grep -q '^image inspect\|^inspect --format\|--platform' "$KPL_TEST_STATE/calls"; then exit 1; fi
grep -q 'stack deploy .*--resolve-image always' "$KPL_TEST_STATE/calls"

# A failed or ambiguous pull cannot change placement, create the network, or
# submit a stack, even when a well-formed digest also appears in its output.
for fault in fail missing malformed uppercase multiple mixed trailing; do
    reset_case
    export KPL_IMAGE=registry.example/kpl:latest KPL_TEST_NO_NETWORK=1 KPL_TEST_PULL_FAULT=$fault
    reject deploy worker-a
    no_mutation
    [ "$(grep -c '^pull ' "$KPL_TEST_STATE/calls")" = 1 ]
    if grep -q '^network \|^node update \|^stack deploy ' "$KPL_TEST_STATE/calls"; then exit 1; fi
done

# Pull timeout accepts literal config values and honors environment overrides.
reset_case
unset KPL_IMAGE
printf 'KPL_IMAGE=registry.example/kpl:v3\nKPL_IMAGE_PULL_TIMEOUT=17\n' > "$scratch/pull-timeout.env"
cp "$scratch/pull-timeout.env" "$scratch/pull-timeout-original.env"
sh "$root/scripts/swarm.sh" --env-file "$scratch/pull-timeout.env" deploy > "$KPL_TEST_STATE/output" 2>&1
grep -Fxq -- '-s TERM -k 5 17 docker pull registry.example/kpl:v3' "$KPL_TEST_STATE/timeouts"
export KPL_IMAGE_PULL_TIMEOUT=23
sh "$root/scripts/swarm.sh" --env-file "$scratch/pull-timeout.env" deploy > "$KPL_TEST_STATE/output" 2>&1
grep -Fxq -- '-s TERM -k 5 23 docker pull registry.example/kpl:v3' "$KPL_TEST_STATE/timeouts"
cmp "$scratch/pull-timeout-original.env" "$scratch/pull-timeout.env"

for invalid_timeout in 0 -1 1.5 08 999999999999999999999999999; do
    reset_case
    export KPL_IMAGE_PULL_TIMEOUT=$invalid_timeout KPL_IMAGE=registry.example/kpl:v3
    reject deploy worker-a
    no_mutation
    [ ! -s "$KPL_TEST_STATE/calls" ]
done

reset_case
unset KPL_API_TOKEN
literal='$(touch "'"$scratch"'/executed")'
printf 'KPL_API_TOKEN=%s\n' "$literal" > "$scratch/literal.env"
sh "$root/scripts/swarm.sh" --env-file "$scratch/literal.env" deploy > "$KPL_TEST_STATE/output" 2>&1
[ ! -e "$scratch/executed" ]
[ "$(cat "$KPL_TEST_STATE/config-token")" = "$literal" ]
if grep -Fq "$literal" "$KPL_TEST_STATE/output"; then exit 1; fi
# Explicit environment values must win over config values without evaluating
# either source. This also exercises a config file with no trailing newline.
printf 'KPL_API_TOKEN=from-file' > "$scratch/override.env"
export KPL_API_TOKEN=from-environment
sh "$root/scripts/swarm.sh" --env-file "$scratch/override.env" deploy > "$KPL_TEST_STATE/output" 2>&1
[ "$(cat "$KPL_TEST_STATE/config-token")" = from-environment ]

reset_case
if sh "$root/scripts/swarm.sh" --env-file "$scratch/missing.env" remove > "$KPL_TEST_STATE/output" 2>&1; then exit 1; fi
no_mutation
[ ! -s "$KPL_TEST_STATE/calls" ]

reset_case
sh "$root/scripts/swarm.sh" --env-file "$scratch/generated.env" init > "$KPL_TEST_STATE/output" 2>&1
[ "$(stat -c '%a' "$scratch/generated.env")" = 600 ]
grep -Eq '^KPL_API_TOKEN=[a-f0-9]{64}$' "$scratch/generated.env"
grep -Eq '^GRAFANA_ADMIN_PASSWORD=[a-f0-9]{64}$' "$scratch/generated.env"
grep -Fxq 'KPL_IMAGE=registry.example.com/kpl-v3:v3' "$scratch/generated.env"
grep -Fxq 'KPL_IMAGE_PULL_TIMEOUT=300' "$scratch/generated.env"
cp "$scratch/generated.env" "$scratch/original.env"
if sh "$root/scripts/swarm.sh" --env-file "$scratch/generated.env" init > "$KPL_TEST_STATE/output" 2>&1; then exit 1; fi
cmp "$scratch/original.env" "$scratch/generated.env"
no_mutation

printf '%s\n' 'PASS: Manager commands enforce cleanup order, stack ownership, failed-task retry safety, literal config, and fresh tag-to-digest resolution before deployment mutations.'
