#!/bin/sh
# Run on the Linux Docker host after building kpl-v3:local.
# All tc changes occur in a disposable container's network namespace.
set -eu

fail() { printf '%s\n' "ERROR: $*" >&2; exit 1; }
[ "$(uname -s)" = Linux ] || fail 'Run this check on the target Linux Docker host.'
command -v docker >/dev/null 2>&1 || fail 'Docker CLI is required.'
command -v timeout >/dev/null 2>&1 || fail 'The timeout utility is required.'
command -v setsid >/dev/null 2>&1 || fail 'The setsid utility is required.'
docker compose version >/dev/null || fail 'Docker Compose v2 plugin is required.'
[ "$(docker info --format '{{.OSType}}')" = linux ] || fail 'A Linux Docker daemon is required.'
security=$(docker info --format '{{json .SecurityOptions}}')
case "$security" in
    *rootless*|*userns*) fail 'The supplied Compose configuration requires rootful Docker without userns-remap; alternate socket/user namespace setups need separate configuration.' ;;
esac

image=${KPL_DOCKER_IMAGE:-kpl-v3:local}
docker image inspect "$image" >/dev/null || fail "Build $image first: docker compose build controller"
printf 'Docker host: %s\n' "$(docker info --format '{{.OperatingSystem}} / {{.KernelVersion}} / {{.Architecture}}')"
printf 'Checking %s with only NET_ADMIN in a private container network...\n' "$image"

# Keep an exact cleanup target even if the CLI fails before returning its ID.
read -r check_uuid < /proc/sys/kernel/random/uuid
container_name=kpl-preflight-$check_uuid
container_id=
cancel_status=0
cleanup() {
    result=$?
    trap - EXIT
    trap '' INT TERM
    if ! timeout -s KILL 120 docker rm -f "$container_name" >/dev/null; then
        printf 'ERROR: Cleanup was not confirmed for %s (ID: %s); inspect/remove this exact container.\n' "$container_name" "${container_id:-unknown}" >&2
        result=1
    fi
    [ "$cancel_status" -eq 0 ] || result=$cancel_status
    exit "$result"
}
trap cleanup EXIT
# A canceled Docker create request can still commit on the daemon. Record
# cancellation, retain the admission result, and only then run cleanup. A
# separate session also shields admission from terminal process-group signals.
trap 'cancel_status=130' INT
trap 'cancel_status=143' TERM
create_status=0
container_id=$(
    trap '' INT TERM
    setsid timeout -s KILL 45 docker create -i --name "$container_name" --network bridge --user 0:0 \
        --cap-drop ALL --cap-add NET_ADMIN --security-opt no-new-privileges:true \
        --read-only --tmpfs /tmp:rw,nosuid,nodev,size=16m \
        --label io.kpl.preflight=true --label "io.kpl.preflight.owner=$container_name" \
        --entrypoint sh "$image" -eu
) || create_status=$?
trap 'exit 130' INT
trap 'exit 143' TERM
[ "$cancel_status" -eq 0 ] || exit "$cancel_status"
[ "$create_status" -eq 0 ] || fail "Docker create failed or exceeded 45 seconds; cleanup target: $container_name"
case "$container_id" in
    ''|*[!0-9a-f]*) fail "Docker returned an invalid container ID; cleanup target: $container_name" ;;
esac
[ "${#container_id}" -eq 64 ] || fail "Docker returned an incomplete container ID; cleanup target: $container_name"

docker start -ai "$container_id" <<'KPL_KERNEL_CHECK'
kpl version
command -v tc
ip -4 address show dev eth0
test -n "$(ip -4 -o address show dev eth0 scope global)"
tc qdisc add dev eth0 root handle 1: prio bands 3 priomap 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0
tc qdisc add dev eth0 parent 1:3 handle 30: netem delay 10ms 1ms distribution normal loss 1%
tc filter add dev eth0 protocol ip parent 1: prio 1 u32 match ip protocol 6 0xff match ip sport 20000 0xffff flowid 1:3
tc filter add dev eth0 protocol ip parent 1: prio 2 u32 match ip protocol 6 0xff match ip dport 20000 0xffff flowid 1:3
tc qdisc add dev eth0 parent 30:1 handle 31: tbf rate 10mbit burst 32kbit latency 50000us
tc qdisc show dev eth0
tc qdisc del dev eth0 root
KPL_KERNEL_CHECK

[ "$(docker inspect --format '{{.State.ExitCode}}' "$container_id")" = 0 ] || fail 'Kernel network check failed; inspect NET_ADMIN and sch_prio/sch_netem/cls_u32/sch_tbf support.'
printf '%s\n' 'PASS: Linux image, Docker access, Compose, prio, netem, u32 filters and TBF.'
