# K-P2PLab v3

[English](README.md) | 한국어

K-P2PLab v3는 여러 호스트에서 표준 libp2p Kademlia와 PubSub 네트워크를 구성하고, 재현 가능한 churn/publish 시나리오를 실행하며, 상태와 전파 이벤트를 웹에서 실시간 관측하는 실험 플랫폼입니다. HopWave는 이 버전의 범위에서 제외했습니다.

코드, 웹 UI, Grafana 대시보드와 문서의 기본 언어는 영어입니다. 한국어 문서는 각 영어 `.md` 파일 옆에 `.kr.md`로 유지합니다. 문서를 수정할 때는 두 언어의 내용을 함께 갱신하십시오.

## 구조

```text
Browser / REST client
        │
        ▼
Controller ─── scenario state machine / registry / event store / dashboard
        │
        ├──────────────┐
        ▼              ▼
     Agent A         Agent B       capacity-aware scheduling / batched telemetry
      │  │            │  │
      ▼  ▼            ▼  ▼
    Peer Peer        Peer Peer     configurable libp2p Kademlia + PubSub
```

- **Controller**: Agent와 Peer 상태, bootstrap registry, 시나리오 실행, 이벤트 저장, REST API와 웹 대시보드를 제공합니다.
- **Agent**: 물리/가상 호스트마다 하나씩 실행합니다. 기본적으로 Peer마다 Docker 컨테이너를 생성해 시작·종료·발행을 담당하고 telemetry를 묶어서 Controller로 전달합니다. 로컬 개발용 process 런타임도 제공합니다.
- **Peer**: 결정적인 `(run ID, seed)` namespace로 ID를 생성하고 선택한 Kademlia와 PubSub 설정으로 실행됩니다. Docker Peer는 별도 network namespace에서 선택적인 네트워크 조건을 적용합니다.
- **Dashboard**: Agent 용량, Peer 준비 상태, 연결 토폴로지, peer score 요약, 실험 단계, 전파 지연과 최근 이벤트를 SSE로 갱신합니다.
- **실험 분석**: 발행 직전 원격 수신 대상을 고정해 churn 도달률, 첫 수신 지연, 평균 중복 수를 집계합니다. Agent 번호로 토폴로지와 상태 표를 연결하고 종료된 Peer는 토폴로지에서 숨깁니다. 웹에서 1~100회 순차 실행, 결과 ZIP 다운로드와 삭제를 지원합니다. [지표 정의와 사용법](docs/experiment-metrics.kr.md)을 참고하십시오.

## 빠른 실행

최종 배포 대상은 Linux입니다. 커널 사전 점검, 원격 접속, 권한 및 정상 종료 순서는 [Linux 운영 가이드](docs/linux-deployment.kr.md)를 참고하십시오. 이미지를 빌드한 후 대상 서버에서 `sh scripts/check-linux.sh`를 실행하며, `make test-linux`로 Linux 컨테이너 안에서 전체 테스트를 실행할 수 있습니다.

Linux 컨테이너를 실행하는 Docker Engine과 Docker Compose 사용을 권장합니다. Compose는 공통 이미지 `kpl-v3:local`을 빌드하고 `kpl-v3-peers` 네트워크에 Controller와 Agent 두 개를 실행합니다. 실험을 시작하면 Agent가 같은 네트워크에 Peer 컨테이너를 생성합니다.

```bash
docker compose up --build
```

브라우저에서 `http://localhost:8080`을 열고 **Run experiment**을 누르면 기본 smoke 시나리오를 실행할 수 있습니다. 변경 API를 보호하려면 실행 전에 환경 변수 `KPL_API_TOKEN`을 설정하고 대시보드 실행 창에도 같은 값을 입력합니다.

화면 포트는 기본적으로 localhost에만 게시합니다. 원격 Linux 서버는 SSH 터널로 접속하거나 Controller의 `KPL_BIND_ADDRESS`/`KPL_HTTP_PORT`를 명시하십시오. 종료할 때 `make stop`을 사용하면 Agent가 살아 있는 동안 Controller가 실험 취소와 Peer 정리를 마칩니다.

```bash
export KPL_API_TOKEN="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
docker compose up --build
```

Peer 컨테이너는 내부 API 포트 `18000`과 P2P TCP 포트 `20000`을 사용하며 호스트에는 해당 포트를 공개하지 않습니다. 각 Agent의 `--self-url`은 Peer 컨테이너에서 접근할 수 있어야 하므로 Compose에서는 `http://agent-a:8090`, `http://agent-b:8090`을 사용하고 Controller 주소는 `http://controller:8080`을 사용합니다.

Compose는 Agent가 형제 Peer 컨테이너를 관리할 수 있도록 Agent에 `/var/run/docker.sock`을 마운트하고 `0:0` 사용자로 실행합니다. Peer에는 Docker socket을 마운트하지 않습니다. 이미지에는 Docker CLI와 `tc`를 제공하는 Alpine [iproute2-tc 패키지](https://pkgs.alpinelinux.org/package/v3.22/main/x86_64/iproute2-tc)가 포함됩니다.

Docker 없이 로컬 개발을 하려면 Go 1.24 이상을 설치하고 `--runtime process`를 명시하십시오. 이 런타임은 노드별 네트워크 조건을 지원하지 않습니다.

```bash
go build -o bin/kpl ./cmd/kpl
./bin/kpl controller --listen :8080
./bin/kpl agent --runtime process --id local-a --advertise-url http://127.0.0.1:8090 --controller-url http://127.0.0.1:8080
```

시나리오 검증:

```bash
./bin/kpl validate --scenario examples/smoke.yaml
```

### 런타임과 여러 호스트 구성

Agent의 기본값은 `--runtime docker`, `--docker-image kpl-v3:local`, `--docker-network kpl-v3-peers`, `--docker-binary docker`입니다. 각 Agent의 Docker daemon에 이미지가 준비되어 있어야 하며 컨테이너에서 Controller와 Agent 주소에 접근할 수 있어야 합니다. `--runtime process`는 기존 로컬 자식 프로세스 방식을 사용합니다.

Agent를 재시작하면 동일 Docker daemon·network에서 같은 Agent ID의 관리 label이 붙은 이전 Peer 컨테이너를 제거합니다. 기존 실험을 복구하거나 재개하지는 않습니다. Agent ID는 같은 daemon·network 내에서 고유해야 합니다. 컨테이너 제거가 일시적으로 실패하면 오류를 보고하며 후속 종료 요청으로 다시 정리할 수 있습니다.

기본 Compose bridge는 같은 Docker 호스트의 컨테이너만 연결합니다. 이미 Linux Swarm에 가입한 서버들은 manager에 이 저장소와 Docker CLI, coreutils의 `timeout`을 준비하십시오. 모든 노드가 접근할 이미지 registry도 필요하며 Swarm 가입으로 registry가 만들어지지는 않습니다. helper는 [stack.swarm.yaml](stack.swarm.yaml)로 선택한 노드마다 Agent 하나와 공통 attachable Peer overlay를 관리합니다.

```sh
sh scripts/swarm.sh nodes
sh scripts/swarm.sh init KPL_IMAGE=registry.example.com/kpl-v3:v3 KPL_AGENT_CAPACITY=20 KPL_MIN_AGENTS=2
sh scripts/swarm.sh config
# registry가 인증을 요구할 때만 실행합니다.
sh scripts/swarm.sh login
sh scripts/swarm.sh publish
# worker가 최소 두 대인 구성입니다. manager 한 대 + worker 한 대이면 --all을 사용합니다.
sh scripts/swarm.sh deploy --workers
sh scripts/swarm.sh check
sh scripts/swarm.sh status
sh scripts/swarm.sh access
sh scripts/swarm.sh credentials
sh scripts/swarm.sh scenario
```

이미지 참조를 기존 registry/repository로 바꾸십시오. Docker에 sudo가 필요하면 로그인과 설정을 포함해 일관되게 `sudo sh scripts/swarm.sh ...`로 실행합니다. `init`은 기존 파일을 덮어쓰지 않고 비공개 설정과 별도 API/Grafana 자격 증명을 생성합니다. 이후 변경은 `configure KEY=VALUE...`, 비밀 값을 가린 유효 설정 조회는 `config`를 사용합니다. 환경변수가 문자 그대로 읽는 설정 파일보다 우선하며, helper는 Compose의 `.env`를 읽거나 daemon의 registry/TLS 설정을 변경하지 않습니다.

`access`의 Controller URL을 열고 **Online Agents 2 이상**이 될 때까지 기다리십시오. **Agent status**에서 `online` 행만 확인하고 여유 슬롯 합계가 6 이상인지 확인합니다. **Peers**는 점유량 / 용량이므로 뒤의 수에서 앞의 수를 빼면 됩니다. 요약의 **Available slots**에는 offline Agent도 포함될 수 있습니다. `status`는 Docker 상태이며 Controller 등록 확인은 아닙니다. **Run experiment**에서 **YAML scenario** 전체를 `scenario` 출력으로 교체하고, `credentials`의 API 토큰을 입력한 뒤 **Run**을 누릅니다. 웹 폼의 초기 YAML은 다른 smoke 실험입니다. Swarm 예제는 Peer 여섯 개를 생성하고 네트워크 조건에서 메시지를 발행한 뒤 `stop-all`을 실행합니다. Grafana는 `credentials`의 별도 계정으로 로그인하고 **Run**에서 실험을 선택합니다.

실험 완료와 Peer 정리를 확인한 뒤 실험 항목 또는 **Saved results**의 **Download results**를 사용하십시오. ZIP에는 저장된 시나리오·메타데이터·이벤트 로그·내보내기 정보가 들어가며 Grafana/Prometheus 시계열 저장소는 포함하지 않습니다. 웹 서비스를 내리기 전에 다운로드합니다.

```sh
sh scripts/swarm.sh remove
```

`deploy`, `add-node`, `remove-node`에는 노드 ID/hostname 목록 또는 선택 옵션 하나를 사용합니다. `--all`은 manager도 포함하고, `--workers`는 worker 역할만 선택하며, `--all-excluding-self`는 현재 Docker context가 가리키는 manager를 제외합니다. self는 해당 daemon의 Swarm Node ID 기준이며 `KPL_CONTROL_NODE_ID`나 셸을 실행하는 서버를 기준으로 정하지 않습니다. 선택 옵션끼리 또는 명시적 노드와 섞을 수 없습니다. 배포·추가는 Linux·Ready·Active 노드를 선택하고 제외한 후보를 안내하며, 철거는 이 stack의 label이 있는 대상만 처리하고 접근 불가 시 실패합니다. 빈 선택도 실패합니다. 기존 label을 보존하고 인자 없는 `deploy`는 이를 재사용하며, 선택은 매 명령마다 평가할 뿐 나중에 가입한 노드에 자동 적용하지 않습니다.

업데이트할 때는 같은 이미지 태그를 유지하고 진행 중 실험을 마친 뒤 `publish`, `deploy` 순서로 실행합니다. 이미지 참조를 변경할 때만 먼저 `configure KPL_IMAGE=...`를 사용하십시오. 매 배포는 최신 registry digest를 확인하고 파일을 바꾸지 않은 채 해당 배포만 고정하므로 **SHA를 수동으로 바꿀 필요가 없습니다**. 아키텍처가 혼합되어 있으면 해당 빌드가 가능한 기존 Buildx builder에서 `publish --platforms linux/amd64,linux/arm64`를 사용하며, 이미지가 manager와 모든 대상 노드를 지원해야 합니다.

`remove`는 Controller 종료 후 Agent 종료와 Peer 정리를 확인합니다. 실패·접근 불가 노드나 확인할 수 없는 task 이력은 자동 철거를 막습니다. 실험·모니터링 volume과 외부 Peer network는 보존합니다. 개별 노드 철거는 담당 Peer를 종료하여 해당 실험에 영향을 줍니다. [전체 Swarm 절차](docs/swarm.kr.md)에서 설정·subnet 선택·문제 확인·보존 결과 및 이전 stack의 별도 마이그레이션 절차를 확인하십시오.

Swarm에서 worker churn을 유지하며 무작위로 반복 발행하려면 [churn·발행 실행 안내](docs/swarm-churn-publish.kr.md)와 [시나리오](examples/swarm-churn-publish.yaml)를 사용하십시오.

### Prometheus와 Grafana

Compose는 Prometheus와 Grafana도 함께 실행하고 데이터 소스와 실험 대시보드를 자동 등록합니다. [Grafana 실험 분석](http://localhost:3000/d/kpl-experiments)에서 실험·Agent·토픽을 선택하고, [Prometheus](http://localhost:9090)에서 직접 쿼리할 수 있습니다. 지표는 5초마다 수집되며 시계열은 named volume에 저장됩니다.

피어 상태, 발행·수신·중복, 전파 지연, churn 실패, 네트워크 설정을 제공합니다. Grafana는 기본적으로 로컬 읽기 전용 열람을 허용하고 관리자 설정은 `.env`에서 지정합니다. [사용법과 지표 해석](docs/monitoring.kr.md), [검증용 실험](examples/monitoring.yaml)을 참고하십시오.

## 모듈 경로

정식 Go 모듈 경로는 `github.com/k-p2p-lab/v3`입니다. 소스 import와 관련 명령에서는 이 경로를 사용해야 합니다.

## 노드 역할, 종류와 프로파일

`role`은 bootstrap discovery를 제어합니다. `boot` 노드는 Controller가 bootstrap peer로 제공하지만 `worker` 노드는 제공하지 않습니다. `type`은 노드의 libp2p, Kademlia, PubSub 동작을 제어합니다. 두 값은 서로 독립적이므로 bootstrap 역할을 오용하지 않고 여러 종류의 worker를 한 실험에서 사용할 수 있습니다. `type`과 `profile`을 모두 생략하면 `role: boot`는 `boot` preset을, 그 밖의 role은 `full`을 선택합니다.

| 내장 type | 동작 |
|---|---|
| `boot` | v2와 마찬가지로 기본 상태에서는 Kademlia만 실행하는 bootstrap 노드입니다. |
| `full` / `worker` | Kademlia server와 표준 GossipSub에 모두 참여하며 v2의 hard connection cap `55`를 사용합니다. |
| `light` | Kademlia client이며 기본 연결 제한이 더 작습니다. |
| `publisher` | 토픽에 구독하지 않고 발행만 수행합니다. |
| `subscriber` / `observer` | 구독 전용이며 발행 요청을 거부합니다. |
| `relay` | 애플리케이션 메시지를 발행하지 않고 구독 토픽 트래픽을 중계합니다. |
| `flood` | FloodSub를 사용합니다. |
| `random` | 최소 degree와 추정 네트워크 크기를 별도로 조정하는 RandomSub를 사용합니다. |
| `dht-only` | PubSub 없이 Kademlia만 실행합니다. |
| `gossip-only` | Kademlia 없이 PubSub만 실행합니다. |
| `non-gossip` / `mesh-only` | lazy gossip을 끈 GossipSub mesh를 사용합니다. |

재사용할 노드 설정은 최상위 `profiles` 맵에 정의합니다. join phase의 설정 결합 순서는 내장 `type`, 이름이 지정된 `profile`, phase 안의 `node` override 순서입니다. 명시적인 `false`와 `0`도 보존하므로 `historyGossip: 0`처럼 lazy gossip을 끄는 실험도 표현할 수 있습니다.

내장 `boot` type은 `gossipsub.enabled: false`를 설정합니다. bootstrap 노드도 PubSub에 참여시킬 때는 profile이나 phase의 `node` 블록에서 이를 명시적으로 `true`로 설정하십시오. 활성화한 boot 노드에는 아래의 모든 PubSub router, parameter, scoring, inspection 옵션을 사용할 수 있습니다. [`examples/mixed-workers.yaml`](examples/mixed-workers.yaml)이 이 opt-in 구성을 보여 줍니다.

```yaml
version: 2
name: mixed-workers
seed: 42
onExit: cancel
jobShutdownTimeout: 3m

profiles:
  tuned-mesh:
    type: full
    libp2p:
      connectionLimit: 128
      connectionManager:
        lowWater: 64
        highWater: 96
        gracePeriod: 30s
    kademlia:
      mode: server
      protocolPrefix: /k-p2p-lab/v3
      bucketSize: 20
      concurrency: 10
    gossipsub:
      router: gossipsub
      topics: [kpl/default]
      floodPublish: false
      peerExchange: true
      params:
        d: 8
        dLow: 6
        dHigh: 12
        dOut: 3
        dLazy: 8
        heartbeatInterval: 500ms

phases:
  - action: join
    group: tuned
    role: worker
    profile: tuned-mesh
    count: 100
    parallel: true
    parallelism: 16
```

### 프로토콜 설정

중첩된 node 설정에서 현재 고정된 버전의 libp2p 패키지가 지원하는 매개변수를 조정할 수 있습니다. 모든 duration 필드는 `250ms`, `30s`, `5m` 같은 Go duration 문자열을 사용합니다.

| 영역 | 조정 가능한 필드 |
|---|---|
| `libp2p` | `userAgent`, `natPortMap`, `relay`, `relayService`, `connectionLimit`, `connectionManager.lowWater`, `connectionManager.highWater`, `connectionManager.gracePeriod`, `dialTimeout` |
| `kademlia` | `enabled`, `mode`, `protocolPrefix`, `protocolId`, `protocolExtension`, `bucketSize`, `concurrency`, `resiliency`, `lookupCheckConcurrency`, routing-table latency/refresh duration, `maxRecordAge`, provider/value/auto-refresh switch, optimistic-provide 설정, bootstrap timeout/retry interval |
| `gossipsub` | `enabled`, `router`, `topicMode`, `randomDegree`, `randomNetworkSize`, `subscribe`, `allowPublish`, `topics`, `floodPublish`, `peerExchange`, message/queue/validation limit, `signaturePolicy`, `seenMessagesTTL`, `subscriptionBufferSize`, `scoreInspectInterval` |
| `gossipsub.params` | 고정된 라이브러리의 모든 `GossipSubParams` 필드: mesh degree, history, gossip factor/retransmission, heartbeat, fanout, prune/backoff, connector, connection timeout, direct-connect 및 opportunistic-graft 제어값, IHAVE 제한, IDONTWANT 제한, IWANT follow-up time |
| `gossipsub.score` | 애플리케이션 정의 P5를 제외한 전체 PeerScore weight와 decay 설정, score threshold, IP-colocation whitelist, mesh·최초 전달·실패·잘못된 메시지 항목을 모두 포함한 `topics.<topic>` score block |

GossipSub 전용 설정(`params`, score/inspection, `floodPublish`, `peerExchange`)은 `gossipsub` router에서만 적용됩니다. FloodSub와 RandomSub는 지원하지 않는 scoring 옵션을 거부하고 각 router 전용 제어값을 사용합니다.

기본 mesh 설정은 `D=6`, `DLow=5`, `DHigh=12`, `DScore=4`, `DOut=2`, `DLazy=6`, history `5/3`, gossip factor `0.25`, heartbeat `1s`입니다. PubSub를 활성화한 `boot` preset은 `DScore=3`을 사용합니다. `full`/`worker`와 역할별 GossipSub worker preset은 v2에서 가져온 `DLow=5`, `DScore=3`, `maxIHaveLength=5500`, 초기 heartbeat `1s`를 상속합니다. 대부분의 worker preset은 v2의 hard connection limit `55`도 사용하며 `light`는 `32`를 사용합니다. Kademlia의 기본 bucket size는 `20`, protocol prefix는 `/k-p2p-lab/v3`입니다. 전체 typed schema와 적용되는 기본값은 [`internal/model/config.go`](internal/model/config.go)를 참고하십시오.

`libp2p.connectionLimit`는 전체 연결 수에 적용되는 resource manager hard cap일 뿐이며 soft connection manager를 암묵적으로 생성하지 않습니다. soft low-water/high-water 정리는 `libp2p.connectionManager` 블록을 명시했을 때만 설치되며 그 블록의 `lowWater`, `highWater`, `gracePeriod`를 사용합니다. 따라서 profile마다 hard cap, soft manager 또는 둘 다를 독립적으로 선택할 수 있습니다.

v2 설정을 이식할 때는 이름과 달리 prefix로 사용됐던 v2 `protocol_id`를 v3 `kademlia.protocolPrefix`로 옮기십시오. v3의 `kademlia.protocolId`는 Kademlia V1 wire protocol ID 전체를 정확히 override하며 `protocolPrefix`, `protocolExtension`과 함께 사용할 수 없으므로 v2 prefix 값을 이 필드에 복사하면 안 됩니다. 기본 prefix `/k-p2p-lab/v3`는 의도적으로 새로 분리한 protocol namespace입니다. v2 namespace 호환이 필요한 실험에서만 `protocolPrefix: /k-p2p-lab/kad-dht`를 사용하십시오.

RandomSub에서 `randomDegree`는 libp2p의 process-global `RandomSubD`를 통해 최소 연결/degree 목표를 설정합니다. 현재 KPL은 Peer마다 별도 프로세스를 사용하므로 이 global 값은 Peer 하나에만 적용됩니다. 여러 Peer를 한 프로세스에 넣는 runtime을 추가한다면 별도 격리가 필요합니다. `randomNetworkSize`는 `NewRandomSub`에 별도로 전달되는 추정 전체 네트워크 크기입니다.

`gossipsub.score`를 활성화하면 `scoreInspectInterval`의 기본값은 `1s`입니다. inspection이 실행될 때마다 노드의 `peerScores` 맵이 갱신됩니다. 이 값은 `/api/v1/nodes`, `/api/v1/network`, `/api/v1/snapshot`, SSE snapshot stream에서 제공되며 topology의 노드에 마우스를 올리면 관측한 score 수와 평균이 표시됩니다.

`appSpecificWeight`는 의도적으로 지원하지 않습니다. PeerScore의 P5 항목에는 프로세스 내부의 애플리케이션 전용 score callback이 필요하지만 직렬화되는 scenario 또는 REST 설정으로는 이를 전달할 수 없습니다. 값은 `0`으로 유지해야 하며, 0이 아닌 값은 효과 없이 받아들이는 대신 설정 검증에서 명시적으로 오류가 됩니다.

### 노드별 네트워크 조건

Docker 런타임에서는 profile 또는 join 단계의 `node` 블록에 `network`를 추가할 수 있습니다. 기본 `scope: p2p`는 각 Peer 컨테이너 안에서 포트 `20000`의 송신 P2P TCP에만 적용합니다. v2처럼 제어 API·telemetry까지 포함한 전체 송신을 제한하려면 `scope: all`을 지정하십시오. `delay`는 편도 egress 추가 지연이며 왕복 지연의 목표값이 아닙니다.

```yaml
node:
  network:
    delay: 100ms
    jitter: 10ms
    lossPercent: 1
    duplicatePercent: 0.1
    corruptPercent: 0.1
    reorderPercent: 1
    rateMbps: 10
    queueLimit: 1000
```

| 필드 | 의미 |
|---|---|
| `delay`, `jitter` | 추가 지연과 변동 폭을 Go duration으로 지정합니다. jitter에는 양수 delay가 필요합니다. |
| `lossPercent`, `duplicatePercent`, `corruptPercent`, `reorderPercent` | `0`~`100` 범위의 패킷 비율입니다. 재정렬에는 양수 delay가 필요합니다. |
| `rateMbps` | 음수가 아닌 송신 속도 제한(Mbps)입니다. `0`은 속도 제한을 해제합니다. |
| `queueLimit` | netem 큐의 최대 패킷 수이며 양의 정수입니다. |
| `scope` | `p2p`(기본) 또는 `all` 송신 트래픽입니다. |
| `delayDistribution` | interval과 같은 분포 형식으로 피어별 기본 지연을 한 번 추출합니다. `delay`와 배타적입니다. |
| `jitterDistribution` | `normal`(기본), `uniform`(v2 방식), `pareto`, `paretonormal`입니다. |
| `reorderCorrelationPercent` | 재정렬 상관계수이며 v2의 `reorder.chance`에 대응합니다. |
| `tbf` | `rateMbps`, `burstKbit`, `latency`로 token bucket을 설정합니다. 양수 netem `rateMbps`와 배타적입니다. |

v2 실험을 옮기실 때는 [재현 검토와 설정 대응표](docs/v2-reproduction.kr.md), [churn 예제](examples/v2-churn.yaml)를 참고하십시오. 여기에는 `placement`, `agentId`, `onError: continue`, 정확한 PubSub 데이터 크기를 위한 `payloadEncoding: raw`, 전체 topic 발행을 위한 `topic: '*'`의 사용법과 남은 차이가 정리되어 있습니다.

네트워크 조건을 적용하는 Peer에는 Linux `NET_ADMIN`과 호스트 커널의 `sch_netem` 지원이 필요합니다. Docker 런타임은 해당 Peer에만 `NET_ADMIN`을 추가합니다. 커널 지원이 없거나 `tc` 명령이 실패하면 노드 시작도 명시적인 오류로 실패합니다. process 런타임 Agent는 공유 호스트 인터페이스를 변경하지 않으며 네트워크 조건이 있는 요청을 거부합니다.

[`examples/network-conditions.yaml`](examples/network-conditions.yaml)은 bootstrap 노드 두 개와 네트워크 조건이 적용된 worker 네 개를 생성하고, 초기화 대기와 메시지 발행 후 모든 노드를 종료합니다. 대시보드에서 실행하거나 다음과 같이 제출할 수 있습니다.

```bash
curl -X POST http://localhost:8080/api/v1/experiments \
  -H 'Content-Type: application/yaml' \
  -H "Authorization: Bearer ${KPL_API_TOKEN:-}" \
  --data-binary @examples/network-conditions.yaml
```

## 시나리오와 잡

권장 시나리오 형식은 version 2 YAML입니다. v2의 중요한 실행 제어를 유지하면서 명시적인 job 추적과 readiness barrier를 추가했습니다. 기존 줄 단위 `.kpl` DSL 자체를 직접 읽지는 않으므로 해당 명령을 phase로 변환해야 합니다.

| Action | 역할 |
|---|---|
| `join` | Agent 용량을 고려하여 노드를 생성합니다. |
| `wait-ready` | 필요하면 지정 job 완료 또는 `minCount` 하한을 먼저 확인한 뒤 그룹/type의 목표 준비율을 기다립니다. |
| `publish` | 준비된 노드를 그룹에서 선택해 메시지를 발행합니다. |
| `leave` | 지정 그룹에서 노드를 선택해 종료합니다. |
| `wait` / `sleep` | 지정 시간 동안 기다립니다. `sleep`은 v2 호환 alias입니다. |
| `wait-jobs` | `jobs`에 지정한 background job을 기다리며, 목록이 비어 있으면 모든 job을 기다립니다. |
| `log` | Controller 로그에 시나리오 메시지를 기록합니다. |
| `stop-all` / `reset` | 모든 background job을 취소하고 종료될 때까지 기다린 뒤 run generation fence를 사용해 해당 실험의 현재 및 이전 Peer generation을 종료합니다. `reset`은 alias입니다. |

`join`에서 `count`는 정확한 생성 작업 수입니다. `publish`와 `leave`에서는 조건에 맞는 후보 노드 수로 제한되는 최댓값이며, publish 후보는 PubSub와 발행이 활성화되어 있고 요청한 topic에 참여해야 합니다. `repeat`는 phase 전체를 반복합니다. `parallel`은 replica의 순차 또는 동시 실행을 선택하며 `parallelism`으로 최대 동시 작업 수를 제한할 수 있습니다. `await`의 기본값은 `true`입니다. `false`로 설정하면 `job` 이름으로 추적되는 background job이 시작되어 churn이나 publish가 진행되는 동안 다음 phase를 실행할 수 있습니다. 이후 `wait-jobs`에서 이름으로 선택해 기다릴 수 있습니다.

phase 목록이 자연스럽게 끝났을 때 background job을 처리하는 방식은 최상위 `onExit`으로 결정합니다. 기본값 `cancel`은 남은 job을 취소한 다음 종료될 때까지 기다립니다. `onExit: drain`은 job이 자연스럽게 완료될 때까지 기다립니다. 자연스럽게 성공한 실행에서는 이 job 정책만 적용하며, 명시적인 `stop-all` phase가 없으면 Peer 프로세스는 계속 실행됩니다.

`jobShutdownTimeout`의 기본값은 `3m`입니다. 사용자 또는 API 요청이 scenario를 취소하거나 phase 또는 background job이 실패하면 Controller는 남은 job을 취소하고 이 제한 안에서 종료를 기다린 뒤, 모든 Agent에 현재 generation까지 generation fence를 설정하고 Peer를 정리하도록 요청합니다. 명시적인 `stop-all`도 같은 제한 시간 내 job 종료를 적용하고 job 추적 상태를 초기화한 뒤 현재 run generation을 fence로 설정합니다. Agent는 일치하는 프로세스를 종료하기 전에 단조 증가하는 fence를 기록합니다. 따라서 generation N의 늦은 create는 fence보다 먼저 완료되어 cleanup에 포함되거나, generation이 fence 이하이므로 거부됩니다. `stop-all`이 성공하면 scenario는 generation N+1로 진행하므로 이후 phase에서 같은 run ID로 새 노드를 만들고 job ID도 다시 사용할 수 있습니다.

`wait-ready`는 실패, 종료 중, 종료된 노드를 포함하여 현재 run generation에서 조건에 맞는 전체 cohort를 검사하며 이전 generation의 노드는 무시합니다. cohort에 실패한 노드가 하나라도 있으면 barrier는 성공하지 않으며, 준비 상태로 보고된 노드도 해당 Agent가 online일 때만 ready 수에 포함됩니다. 이 cohort 역시 지금까지 관측된 노드로만 구성되므로 `await: false` join 다음의 `wait-ready`에는 `jobs: [job-id]` 또는 `minCount` 중 하나가 필요합니다. `jobs`는 지정 producer job이 완료된 뒤 readiness를 검사합니다. `minCount`는 producer를 계속 실행하면서 일부만 생성된 그룹이 준비율을 너무 일찍 만족하지 못하게 합니다.

| `parallel` | `await` | 동작 |
|---|---|---|
| `false` | `true` | 간격을 적용한 replica를 순차 실행하며 다음 phase가 기다립니다. |
| `true` | `true` | replica를 동시에 실행하며 다음 phase가 전체 batch를 기다립니다. |
| `false` | `false` | 간격을 적용한 순차 job이 background에서 실행됩니다. |
| `true` | `false` | 동시 실행 job이 background에서 실행됩니다. |

v2 호환을 위해 `interval`을 생략한 `publish` phase에는 특수 기본값을 적용합니다. `parallel: true`이면 각 작업에 phase 시작 기준 `1s` offset을 주며, 순차 publish의 delay는 `0`입니다. 이 규칙은 `await` 값과 무관합니다.

`wait` duration과 readiness/job timeout은 양수여야 합니다. join의 `lifetime`을 생략하면 자동 leave를 하지 않지만 명시적으로 sample된 `0s` lifetime은 v2와 같이 새 노드를 즉시 종료합니다.

```yaml
phases:
  - name: background churn
    action: join
    job: churn
    await: false
    parallel: false
    group: churners
    role: worker
    type: light
    count: 100
    interval: {model: exponential, mean: 250ms}
    lifetime: {model: pareto, xm: 45s, alpha: 2.5, max: 3m}

  - action: wait
    duration: 10s

  - action: wait-ready
    group: churners
    jobs: [churn]
    readyRatio: 1
    timeout: 5m
```

간격과 수명에는 다음 분포를 사용할 수 있습니다. duration 값은 Go duration 문자열을 사용하며 선택적인 `min`/`max` duration 범위로 결과를 제한할 수 있습니다.

| Model | 매개변수와 sampling 의미 |
|---|---|
| `fixed` | `value`가 정확한 duration입니다. |
| `exponential` | `mean`은 연속 지수분포 sample의 평균 duration입니다. |
| `normal` | `mean`과 `sigma`는 duration 단위의 평균과 표준편차입니다. 음수 sample은 0으로 제한한 뒤 선택적인 범위를 적용합니다. |
| `pareto` | `xm`은 scale duration이며 `alpha`는 무차원 shape입니다. |
| `poisson` | v2 호환 동작입니다. duration인 `mean`을 초 단위 Poisson 평균으로 변환하며 sample 결과는 정수 초로 양자화됩니다. |
| `gamma` | `alpha`는 shape입니다. v2 호환 `beta`(초당 rate) 또는 `scale`(duration) 중 정확히 하나만 사용해야 하며 두 형식은 상호배타적입니다. |
| `lognormal` | `mu`는 log-space 평균이고 sigma는 무차원입니다. `sigma: "0.5"` 같은 숫자 문자열 또는 숫자 필드 `logSigma` 중 정확히 하나를 사용합니다. 표준정규분포 `Z`에 대해 결과는 `exp(mu + sigma*Z)`초입니다. |

0이 아닌 scenario seed는 초기 상태, 조건에 맞는 후보 집합, readiness/job barrier가 같을 때 sample된 delay와 분포 sample, 무작위 선택 및 순서를 재현합니다. Peer identity는 `(run ID, 노드별 seed)` 조합마다 결정됩니다. 같은 조합은 같은 Peer ID를 만들지만, run ID가 다르면 실행 간 identity 충돌을 막기 위해 의도적으로 다른 Peer ID를 만듭니다. 따라서 같은 scenario seed는 실행 간 sampling과 순서를 재현하지만 run ID가 다르면 Peer ID까지 같아지지는 않습니다. `seed: 0`은 의도적으로 실행 시각 기반 seed를 선택합니다. 먼저 [`examples/smoke.yaml`](examples/smoke.yaml)을 확인하고, custom profile, 이종 worker type, 제한된 병렬 batch, background paced job은 [`examples/mixed-workers.yaml`](examples/mixed-workers.yaml)을 참고하십시오.

## REST API

| Method | Path | 설명 |
|---|---|---|
| `GET` | `/api/v1/health` | Controller 상태 확인 |
| `GET` | `/api/v1/snapshot` | 노드 `peerScores`를 포함한 대시보드 전체 snapshot |
| `GET` | `/api/v1/agents` | Agent 상태 |
| `GET` | `/api/v1/nodes` | inspection으로 수집한 `peerScores`를 포함한 Peer 상태 |
| `GET` | `/api/v1/network` | `peerScores`가 포함된 Peer, 연결 edge와 전파 지표 |
| `GET` | `/api/v1/bootstrap?runId={runId}` | 필수 run ID에 속한 준비 상태의 bootstrap peer만 반환 |
| `GET` | `/api/v1/events` | 최근 trace event |
| `GET` | `/api/v1/stream` | `peerScores`를 포함한 실시간 snapshot SSE |
| `GET` | `/api/v1/experiments` | 실험 상태와 `activeJobs`, `completedJobs`, `failedJobs`, `canceledJobs` counter |
| `GET` | `/api/v1/results` | 이전 Controller 실행에서 저장한 실험을 포함하는 결과 목록 |
| `DELETE` | `/api/v1/results/{id}` | 비활성 저장 결과 삭제. 진행 중 배치·다운로드 보호 |
| `GET` | `/api/v1/experiments/{id}/download` | 저장된 시나리오·메타데이터·수집 이벤트를 ZIP으로 다운로드 |
| `POST` | `/api/v1/experiments` | YAML 1회 실행 또는 JSON `{scenario, repetitions}`로 1~100회 순차 실행 |
| `POST` | `/api/v1/experiments/{id}/stop` | 실행을 취소한 뒤 제한 시간 내 job 종료와 generation-fenced Peer cleanup 수행 |

`/api/v1/bootstrap`의 `runId` query parameter는 필수입니다. registry는 해당 run에서 준비 상태이고 유효한 identity와 address 정보가 있는 `boot` 노드만 반환하므로 동시에 실행되는 실험끼리 bootstrap peer를 발견하지 않습니다.

예시:

```bash
curl -X POST http://localhost:8080/api/v1/experiments \
  -H 'Content-Type: application/yaml' \
  -H "Authorization: Bearer ${KPL_API_TOKEN:-}" \
  --data-binary @examples/smoke.yaml
```

원시 이벤트는 `data/runs/<run-id>/events.jsonl`, 실행 입력은 같은 디렉터리의 `scenario.yaml`, 실험 메타데이터는 `experiment.json`에 저장됩니다. Compose/Swarm에서는 영구 `controller-data` 볼륨을 `/var/lib/kpl/data`에 마운트하며, 실험별 파일은 그 아래 `runs/<run-id>`에 저장됩니다.

Control Room의 **Download results**로 실험 결과를 ZIP으로 받을 수 있습니다. **Saved results**에는 이전 Controller 실행에서 보존된 결과도 표시되며, **Refresh**로 목록을 다시 읽습니다. 실행 중 실험의 **Download snapshot**은 다운로드 시작 시점까지 저장된 기록을 담습니다. 최근 300개 이벤트 버퍼와 별개로 저장된 전체 이벤트 로그를 내보냅니다. 파일 구성과 수집 한계는 [실험 결과 다운로드](docs/monitoring.kr.md#실험-결과-다운로드)를 참고하십시오.

Agent는 Controller가 cleanup에 사용하는 다음 내부 운영 endpoint를 제공합니다.

| Method | Agent path | 설명 |
|---|---|---|
| `DELETE` | `/api/v1/runs/{runId}/nodes?generation=N` | unsigned `generation`이 필수이며, run fence를 N까지 원자적으로 높이고 이후 generation N 이하의 create를 거부하며 해당 generation의 기존 노드를 종료한 뒤 `202 Accepted`를 반환합니다. |

이 endpoint와 Controller와 Agent 사이의 등록, heartbeat, 노드 lifecycle, publish, batch telemetry endpoint는 REST/JSON을 사용합니다. 이들은 client-facing Controller API가 아닌 내부 cluster·운영 API이며 별개로 변경될 수 있습니다.

`KPL_API_TOKEN`은 KPL 변경 API에 쓰는 공통 Bearer 토큰이며 Swarm join token, Docker 권한, Grafana 비밀번호와는 별개입니다. Controller와 모든 Agent에 같은 값을 설정하면 Agent가 Peer에도 전달합니다. Swarm stack에서는 필수이고 Compose/CLI에서는 선택 사항입니다. 빈 값이면 토큰 검사를 비활성화하며, 사용자·역할별 권한 분리는 없습니다.

대시보드의 **Run experiment → API token**에 같은 값을 입력하십시오. 실행 버튼을 누르면 해당 origin의 브라우저 `localStorage`에 저장하여 이후 실행·중지 요청에 사용하며 자동 만료되지 않습니다. REST 요청에는 `Authorization: Bearer <token>`을 붙입니다. 상태·이벤트·SSE·metrics 등 GET 조회는 토큰 설정 후에도 공개입니다. Controller는 HEAD도 인증 검사에서 제외하고 Agent와 Peer는 GET만 제외합니다. 토큰 자체가 HTTP 전송을 암호화하지는 않습니다.

같은 네 가지 job counter가 `/api/v1/snapshot`과 SSE snapshot에도 포함됩니다. 대시보드는 각 run에 이를 표시하므로 Controller 로그를 열지 않아도 실행 중, 성공, 실패, 취소된 background 작업 수를 확인할 수 있습니다.

## 측정 주의사항과 한계

전파 메시지에는 발행 시각이 포함됩니다. 서로 다른 물리 서버의 지연을 비교하려면 모든 Agent 호스트에 chrony/NTP를 적용해야 하며, 음수 지연이 감지되면 이벤트에 `clockSkewDetected`가 기록됩니다.

Docker 런타임은 Peer마다 네트워크를 격리하고 노드별 P2P egress 조건을 지원합니다. `wait-ready`는 Peer 초기화와 API 준비 완료를 확인하며 mesh 수렴을 검증하지 않습니다. 필요한 실험에는 별도 안정화 대기 단계를 추가하십시오. scenario seed는 앞서 설명한 조건에서 애플리케이션 sampling과 순서를 재현하지만, 커널의 packet impairment나 네트워크 타이밍까지 동일하게 재현하지는 않습니다. HopWave는 지원하지 않습니다.

종료된 노드의 이력은 현재 메모리에 유지되며 Agent heartbeat와 Controller snapshot에도 포함됩니다. 장기간 대규모 churn을 실행할 때 control-plane 상태와 payload가 계속 증가하지 않도록 제한된 보존 정책과 별도의 pagination 기반 history API가 추가로 필요합니다.
