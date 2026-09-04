# Swarm 다중 서버 배포와 분배 검토

`compose.yaml`은 단일 Docker 호스트용이며, 다중 서버에는 `stack.swarm.yaml`을 사용합니다. Swarm은 **서버마다 Agent 하나**를 유지하고, Controller가 실험의 Peer를 Agent에 배분합니다. Peer는 해당 서버의 독립 Docker 컨테이너입니다. Swarm 서비스처럼 Peer를 다른 서버로 자동 재배치하지 않습니다.

## 배포 전 준비

Linux rootful Docker 서버들을 같은 Swarm에 가입시켜야 합니다. 서버 간 TCP 2377(manager), TCP/UDP 7946, UDP 4789 통신이 필요합니다. VXLAN 포트는 신뢰하는 클러스터 서버 사이에서만 허용하십시오. VPN·클라우드망에서는 underlay MTU와 VXLAN overhead도 확인해야 합니다. [Docker overlay 요구사항](https://docs.docker.com/engine/network/drivers/overlay/)

실험에 쓸 노드에만 `kpl.agent=true` label을 설정합니다. 저장소가 있는 Controller 노드의 **Node ID**를 고정합니다. Controller·Prometheus·Grafana의 로컬 volume이 빈 다른 서버로 이동하는 것을 막기 위한 설정입니다. control 노드에도 Agent를 실행하면 실험 부하와 분석 부하가 같은 CPU·메모리를 사용합니다.

```sh
docker node ls
docker node update --label-add kpl.agent=true worker-a
docker node update --label-add kpl.agent=true worker-b

export KPL_STACK_NAME=kpl
export KPL_CONTROL_NODE_ID="$(docker node inspect --format '{{.ID}}' manager-a)"
export KPL_PEER_NETWORK=kpl-swarm-peers
export KPL_AGENT_CAPACITY=20
# 모든 서버에서 접근 가능한 registry에 같은 버전의 이미지를 push한 뒤 digest를 지정합니다.
export KPL_IMAGE='registry.example.com/lab/kpl@sha256:REPLACE_WITH_DIGEST'
export KPL_API_TOKEN='REPLACE_WITH_SHARED_TOKEN'
export GRAFANA_ADMIN_PASSWORD='REPLACE_WITH_ADMIN_PASSWORD'

# 주소가 서버/LAN/VPN/기존 Docker 네트워크와 겹치지 않는지 먼저 확인합니다.
docker network create --driver overlay --attachable "$KPL_PEER_NETWORK"
sh scripts/check-swarm.sh
docker stack config --compose-file stack.swarm.yaml >/dev/null
docker stack deploy --with-registry-auth --compose-file stack.swarm.yaml "$KPL_STACK_NAME"
docker stack services "$KPL_STACK_NAME"
docker service ps "${KPL_STACK_NAME}_agent" --no-trunc
```

Swarm의 `stack deploy`는 로컬 소스 빌드와 Compose의 `.env` 자동 로드를 수행하지 않습니다. 위 변수는 셸에 export해야 하며, 기존 `.env`를 만들었다는 이유로 적용되지는 않습니다. 혼합 amd64/arm64 서버에서는 두 아키텍처를 포함한 이미지 manifest digest가 필요합니다. Agent 시작 시 자신의 실제 로컬 image ID를 조회해 Peer에 사용하므로 같은 서버의 Agent·Peer 바이너리가 일치합니다. [Swarm stack 배포](https://docs.docker.com/engine/swarm/stack-deploy/)

Agent는 worker의 Docker socket만 사용하며 manager API 접근이 필요 없습니다. `{{.Service.Name}}-{{.Node.ID}}`로 ID를 만들고 자신의 task가 연결된 Peer overlay IPv4를 `advertise-url`과 `self-url`로 사용합니다. 서비스 VIP나 공통 DNSRR 주소를 개별 Agent 주소로 지정하면 다른 서버가 요청을 받을 수 있습니다. Agent는 peers overlay에 직접 연결되어 있으므로 worker에서 overlay가 준비된 뒤 실행됩니다. [Swarm global 서비스와 템플릿](https://docs.docker.com/engine/swarm/services/)

별도 stack은 고유한 `KPL_STACK_NAME`과 `KPL_PEER_NETWORK`를 사용하십시오. 배포 명령의 stack 이름은 `KPL_STACK_NAME`과 같아야 합니다. Controller 주소는 `${KPL_STACK_NAME}_controller`로 지정하여 공유 DNS alias의 혼선을 피합니다.

## 분배와 용량

| 설정 | 배치 동작 |
|---|---|
| `placement: balanced` (기본) | 점유량/Agent capacity가 가장 낮은 online Agent부터 배치. 동률은 Agent ID 순서 |
| `placement: random` | 여유가 있는 online Agent 중 시드 기반 선택 |
| `placement: single-agent` | 한 join 실행을 선택한 Agent 하나에 고정. repeat는 회차마다 재선택. 가득 차면 다른 서버로 넘기지 않고 대기 |
| `agentId` | 지정한 Agent로 고정. 물리 서버 고정 실험은 `/api/v1/agents`의 ID 사용 |
| `parallelism` | `parallel: true`인 join에서 동시에 실행할 작업 수. 서버 수나 Swarm replica 수와 다름 |

capacity는 Peer 개수의 admission 제한이며 CPU·메모리 예약이나 cgroup 한도가 아닙니다. Agent의 Swarm resource 제한을 바꾸어도 형제 컨테이너인 Peer에 전파되지 않습니다. 이질적인 서버는 Agent 서비스 그룹을 분리해 서로 배타적인 node label과 다른 capacity를 적용할 수 있으며, 한 노드에 여러 Agent가 겹치지 않도록 해야 합니다. CPU/RAM/FD/conntrack 및 Docker daemon 부하를 측정해 capacity를 정하십시오.

동시 실험의 예약은 Controller에서 직렬화합니다. Agent는 Docker 생성부터 **삭제 완료 확인까지** 슬롯을 유지하고, 삭제 실패도 점유량에 포함합니다. Controller는 DELETE 응답만으로 슬롯을 재사용하지 않습니다. 네트워크 단절로 보고가 10초 넘게 없으면 신규 배치에서 제외하며, 이전 Agent 프로세스나 순서가 뒤집힌 보고가 현재 노드·용량을 덮어쓰지 않도록 검사합니다. 같은 ID의 새 Agent는 이전 프로세스의 online 유효 시간이 끝난 뒤 등록됩니다.

Docker 생성 admission과 생성 이후 설정 복사·시작·주소 확인에는 각각 45초의 예산을 적용합니다. 데몬의 생성 대기가 시작 단계 예산까지 소모하지 않도록 분리했습니다. 원래 요청의 더 짧은 deadline과 취소는 계속 적용됩니다. 혼잡한 서버에서 이 제한을 반복해서 초과하면 join `parallelism`과 capacity를 낮추고 디스크·데몬 부하를 점검하십시오.

Docker는 일반적인 overlay에 `/24` 규모를 권장합니다. 주소에는 Peer뿐 아니라 서비스 task·endpoint도 포함되므로 `서버 수 × capacity`를 주소 수만큼 꽉 채우지 마십시오. 기본 20은 시작용 설정이며 성능 보장이 아닙니다. v2의 `/16`을 그대로 확대하거나 capacity만 올리는 방식으로 수천 Peer 지원을 주장할 수 없습니다. 현재 구성은 단일 공통 Peer overlay를 사용하므로 수백·수천 Peer에는 여러 네트워크 간 도달성 설계와 별도 부하 시험이 필요합니다. [Swarm overlay 크기 제한](https://docs.docker.com/engine/swarm/networking/#overlay-network-size-limitations)

Peer는 Agent로 향하는 경로의 overlay IPv4만 libp2p에 광고합니다. 다른 서버에서 접근할 수 없는 `docker_gwbridge` 주소가 bootstrap/Identify에 섞이지 않게 합니다. tc 지연·손실은 Peer namespace의 송신에 적용되며, 실제 NIC·VXLAN·서버 부하의 추가 지연은 그대로 존재합니다. 호스트 간 one-way latency 비교에는 시계 동기화도 필요합니다.

## 분석과 운영

Prometheus는 private monitoring overlay의 `tasks.agent`를 DNS로 조회하여 각 Agent를 개별 수집합니다. 서버 추가·task 재시작 시 5초 간격으로 탐색합니다. Grafana는 기존 KP2PLab 대시보드를 자동 구성합니다. 다음 항목을 함께 확인하십시오.

- `up{job="kpl-agent"}` 대상 수와 실제 Agent task 수
- Controller의 `kpl_agent_active_nodes`, `kpl_agent_capacity`, `kpl_agent_up`
- Agent의 `kpl_local_cleanup_pending`, `kpl_local_telemetry_queue_events`
- 실험의 실제 ready/started 시각, 실패 수 및 서버별 CPU·메모리·네트워크 사용량

Go process 지표는 Controller·Agent 자체만 나타내며 Peer 전체의 자원 사용량이 아닙니다. `docker stats`, 호스트 모니터링 또는 별도 container exporter로 Peer 부하를 확인하십시오. 전체 노드 상태 보고·Controller 저장/집계·중앙 HTTP 수집, Docker CLI의 생성/삭제 비용도 확장 한계입니다. 종료된 노드 기록과 실행별 Prometheus series가 유지되므로 긴 churn 실험은 메모리와 수집 지연을 함께 측정해야 합니다.

Swarm의 published port는 Compose의 `127.0.0.1` 바인딩과 다릅니다. 이 stack은 host mode로 control 노드의 8080·9090·3000을 게시합니다. 관리망 방화벽이나 인증 reverse proxy로 접근을 제한하십시오. `KPL_API_TOKEN`은 변경 API만 보호하고 GET 조회는 보호하지 않습니다. Grafana 익명 접속은 이 stack에서 비활성화했습니다. [Swarm host mode 게시](https://docs.docker.com/engine/swarm/services/#publish-ports)

모니터링 설정은 Swarm configs로 전달하므로 모든 서버에 저장소를 복사할 필요가 없습니다. Swarm config는 immutable이므로 설정 파일 변경 시 `stack.swarm.yaml`의 config key와 해당 참조를 함께 버전명으로 바꾸어 새 config를 배포하십시오. 데이터 volume은 같은 control Node ID에서 유지됩니다.

## 장애·업데이트·종료

Agent update/rollback은 `stop-first`입니다. `start-first`로 바꾸면 같은 서버·Agent ID의 두 task가 동시에 실행되어 이전 Peer 정리와 충돌할 수 있습니다. Agent 교체 시 담당 Peer가 종료되므로 진행 중인 실험의 무중단 업그레이드를 보장하지 않습니다. 서버를 추가해도 이미 실행 중인 Peer를 옮기지 않으며, 이후 join부터 새 용량을 사용합니다.

노드 drain은 Swarm 서비스 task에 적용됩니다. v3 Peer는 standalone 컨테이너이므로 Swarm이 직접 이전하지 않습니다. 정상 Agent 종료에서 Peer를 정리하고, 비정상 종료 후에는 동일 Agent ID·Peer network name으로 같은 노드에서 재시작할 때 잔존 컨테이너를 회수합니다. 노드 재가입, stack/service 이름 변경 또는 네트워크 이름 변경 시 이전 소유 범위의 잔존 Peer는 해당 서버에서 label을 확인해 별도로 정리해야 합니다. 전체 호스트 정지·네트워크 단절을 다른 서버의 Peer 재생성으로 숨기지 않습니다.

Controller는 단일 인스턴스이며 공유 DB/leader election을 구현하지 않았습니다. `replicas: 1`을 유지하십시오. control 노드 장애 시 자동으로 빈 로컬 volume을 쓰는 다른 노드로 이동하지 않으며, 백업 복원과 새 Node ID 지정이 필요합니다. Controller는 저장된 실험·counter를 시작 시 메모리에 복원하거나 실행 중 실험을 자동 재개하지 않습니다. 파일은 분석용으로 보존됩니다. Controller crash만으로 Agent의 Peer가 종료되지는 않습니다. 계획된 업데이트 전 시나리오의 `stop-all` 또는 활성 run 취소로 정리하고 잔존 Peer를 확인하십시오. 이미 completed이지만 `stop-all`을 생략한 run의 Peer는 Controller 종료만으로 정리되지 않습니다.

```sh
# 먼저 Controller의 실험 취소와 Peer 정리가 끝날 때까지 기다립니다.
docker service scale --detach=false "${KPL_STACK_NAME}_controller=0"
docker service ps "${KPL_STACK_NAME}_controller" --no-trunc
# Controller task가 실제 종료되고 Peer 정리가 확인된 뒤 실행합니다.
docker stack rm "$KPL_STACK_NAME"
```

## 검증 범위

대상 서버마다 `docker pull "$KPL_IMAGE"` 후 `KPL_DOCKER_IMAGE="$KPL_IMAGE" sh scripts/check-linux.sh`로 커널 기능을 확인하고, manager에서 `sh scripts/check-swarm.sh`로 기본 배포 조건을 확인하십시오. Linux 커널 검사 스크립트는 Docker Compose v2, `timeout`, `setsid`도 필요합니다. 이 검사들은 cross-host VXLAN 실제 통신이나 처리량을 대신하지 않습니다.

최소 두 서버에서 [분산 실험 예제](../examples/swarm-smoke.yaml)를 실행해 서로 다른 Agent에 ready Peer가 생기는지, 광고 주소가 Peer overlay에 속하는지, publish/deliver가 양쪽 서버에서 발생하는지 확인하십시오. 예제는 boot 1개와 worker 5개를 생성하므로 총 capacity 6 이상이 필요합니다. 그 뒤 목표 Peer 수를 단계적으로 올려 ready 지연·실패율·분배·자원 사용량을 기록하십시오. 네트워크 단절/Agent 재시작 시 stale 제외·잔존 컨테이너 정리와 Prometheus 대상 교체도 확인해야 합니다.

### 2026-09-04 실행 검증

검증 환경은 **동일 WSL2 Linux 커널 위의 격리된 Docker 데몬 두 개**입니다. Docker 29.7.2의 manager·worker와 실제 attachable overlay를 구성했습니다. 기존 호스트의 Swarm 상태는 변경하지 않았습니다. 이는 두 데몬 사이의 배치·VXLAN·서비스 탐색 검증이며, 서로 다른 물리 서버의 NIC·MTU·장애·대규모 처리량 검증은 아닙니다. 이미지는 양쪽 데몬에 같은 로컬 이미지를 미리 적재했으며 registry 인증·pull 경로는 별도 검증 대상입니다.

| 항목 | 결과 |
|---|---|
| Linux 코드 검증 | 전체 Go 테스트 통과. 마지막 시간 예산 수정 후 Agent 패키지와 Swarm shell 회귀 테스트 재검증 통과 |
| Swarm 배포 | global Agent 2개, Controller 1개, Prometheus 1개, Grafana 1개 정상 실행. 실제 manager에서 사전 검사 통과 |
| 노드 분배 | capacity 4인 Agent 두 개에 Peer 6개를 3개씩 배치, 모두 ready 및 상호 연결 확인 |
| Peer 주소 | 모든 광고 주소가 Peer overlay IPv4의 TCP 20000 한 개. gateway bridge 주소 미포함 |
| 네트워크·발행 | worker 25ms 지연·2ms jitter·0.5% 손실, 4096-byte payload 50회 발행, 수신 300회(Agent별 150회) |
| 모니터링 | Prometheus의 Agent 대상 2개와 Controller·자기 수집 모두 UP. Grafana 대시보드 UID `kpl-experiments`와 데이터소스 API 연결 OK |
| task 교체 | 이미지 업데이트 후 Node 기반 Agent ID 유지, task IP 변경 및 Prometheus 대상 갱신 확인 |
| 정상 종료 | `run-20260904T040249Z-34d32bbb9559adbba6519a558c6cc107` completed, 양쪽 activeNodes 0, 실제 잔존 Peer 컨테이너 각각 0개 |

발행·수신 수는 Controller의 누적 `kpl_events_total` 기준이며, 300개 최근 이벤트만 사용하는 snapshot 요약 수치와 구분합니다. 첫 부하 실행에서 생성·시작 전체의 공유 45초 제한이 Peer를 종료시키는 문제를 발견해 단계별 예산으로 수정한 뒤 위 결과를 확인했습니다.
