# K-P2PLab v3

한국어 | [English](README.md)

K-P2PLab v3는 여러 호스트에서 표준 libp2p Kademlia와 PubSub 네트워크를 구성하고, 재현 가능한 churn/publish 시나리오를 실행하며, 상태와 전파 이벤트를 웹에서 실시간 관측하는 실험 플랫폼입니다. HopWave는 이 버전의 범위에서 제외했습니다.

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

## 빠른 실행

Linux 컨테이너를 실행하는 Docker Engine과 Docker Compose 사용을 권장합니다. Compose는 공통 이미지 `kpl-v3:local`을 빌드하고 `kpl-v3-peers` 네트워크에 Controller와 Agent 두 개를 실행합니다. 실험을 시작하면 Agent가 같은 네트워크에 Peer 컨테이너를 생성합니다.

```bash
docker compose up --build
```

브라우저에서 `http://localhost:8080`을 열고 **실험 실행**을 누르면 기본 smoke 시나리오를 실행할 수 있습니다. API 보호가 필요하면 실행 전에 환경 변수 `KPL_API_TOKEN`을 설정하고 대시보드 실행 창에도 같은 값을 입력합니다.

```bash
export KPL_API_TOKEN='replace-me'
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

기본 Compose bridge는 같은 Docker 호스트의 컨테이너만 연결합니다. 여러 호스트에서는 Docker 호스트를 같은 Swarm에 참여시키고 공통 attachable overlay 네트워크를 사용하십시오. 예를 들어 Swarm manager에서 `docker network create --driver overlay --attachable kpl-v3-peers`를 실행합니다. 각 Agent의 `--docker-network`를 해당 네트워크로 지정하고 Controller와 Agent도 여기에 연결하며, 고유한 Agent ID와 접근 가능한 `--advertise-url`, `--self-url`, `--controller-url`을 설정해야 합니다. 미리 생성한 overlay에 Compose를 연결할 경우 `networks.peers` 정의를 `name: kpl-v3-peers`, `external: true`로 바꾸십시오. 호스트별 bridge만으로는 호스트 간 Peer 연결이 되지 않습니다. 세부 조건은 [Docker overlay 네트워크 문서](https://docs.docker.com/engine/network/drivers/overlay/)를 참고하십시오.

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
jobShutdownTimeout: 30s

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

Docker 런타임에서는 profile 또는 join 단계의 `node` 블록에 `network`를 추가할 수 있습니다. Linux `tc netem`은 각 Peer 컨테이너 안에서 출발지 또는 목적지 포트가 `20000`인 송신 P2P TCP 패킷에만 설정을 적용합니다. Controller, Agent, Peer의 HTTP 제어 트래픽에는 적용하지 않습니다. `delay`는 편도 egress 추가 지연이며 왕복 지연의 목표값이 아닙니다.

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

`jobShutdownTimeout`의 기본값은 `30s`입니다. 사용자 또는 API 요청이 scenario를 취소하거나 phase 또는 background job이 실패하면 Controller는 남은 job을 취소하고 이 제한 안에서 종료를 기다린 뒤, 모든 Agent에 현재 generation까지 generation fence를 설정하고 Peer를 정리하도록 요청합니다. 명시적인 `stop-all`도 같은 제한 시간 내 job 종료를 적용하고 job 추적 상태를 초기화한 뒤 현재 run generation을 fence로 설정합니다. Agent는 일치하는 프로세스를 종료하기 전에 단조 증가하는 fence를 기록합니다. 따라서 generation N의 늦은 create는 fence보다 먼저 완료되어 cleanup에 포함되거나, generation이 fence 이하이므로 거부됩니다. `stop-all`이 성공하면 scenario는 generation N+1로 진행하므로 이후 phase에서 같은 run ID로 새 노드를 만들고 job ID도 다시 사용할 수 있습니다.

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
| `POST` | `/api/v1/experiments` | YAML 시나리오 실행 |
| `POST` | `/api/v1/experiments/{id}/stop` | 실행을 취소한 뒤 제한 시간 내 job 종료와 generation-fenced Peer cleanup 수행 |

`/api/v1/bootstrap`의 `runId` query parameter는 필수입니다. registry는 해당 run에서 준비 상태이고 유효한 identity와 address 정보가 있는 `boot` 노드만 반환하므로 동시에 실행되는 실험끼리 bootstrap peer를 발견하지 않습니다.

예시:

```bash
curl -X POST http://localhost:8080/api/v1/experiments \
  -H 'Content-Type: application/yaml' \
  --data-binary @examples/smoke.yaml
```

원시 이벤트는 `data/runs/<run-id>/events.jsonl`, 실행 입력은 같은 디렉터리의 `scenario.yaml`에 저장됩니다.

Agent는 Controller가 cleanup에 사용하는 다음 내부 운영 endpoint를 제공합니다.

| Method | Agent path | 설명 |
|---|---|---|
| `DELETE` | `/api/v1/runs/{runId}/nodes?generation=N` | unsigned `generation`이 필수이며, run fence를 N까지 원자적으로 높이고 이후 generation N 이하의 create를 거부하며 해당 generation의 기존 노드를 종료한 뒤 `202 Accepted`를 반환합니다. |

이 endpoint와 Controller와 Agent 사이의 등록, heartbeat, 노드 lifecycle, publish, batch telemetry endpoint는 REST/JSON을 사용합니다. 이들은 client-facing Controller API가 아닌 내부 cluster·운영 API이며 별개로 변경될 수 있습니다. `KPL_API_TOKEN`을 설정한 경우 상태를 변경하는 API 호출에 `Authorization: Bearer <token>`이 필요하며 GET과 HEAD는 read-only로 유지됩니다.

같은 네 가지 job counter가 `/api/v1/snapshot`과 SSE snapshot에도 포함됩니다. 대시보드는 각 run에 이를 표시하므로 Controller 로그를 열지 않아도 실행 중, 성공, 실패, 취소된 background 작업 수를 확인할 수 있습니다.

## 측정 주의사항과 한계

전파 메시지에는 발행 시각이 포함됩니다. 서로 다른 물리 서버의 지연을 비교하려면 모든 Agent 호스트에 chrony/NTP를 적용해야 하며, 음수 지연이 감지되면 이벤트에 `clockSkewDetected`가 기록됩니다.

Docker 런타임은 Peer마다 네트워크를 격리하고 노드별 P2P egress 조건을 지원합니다. `wait-ready`는 Peer 초기화와 API 준비 완료를 확인하며 mesh 수렴을 검증하지 않습니다. 필요한 실험에는 별도 안정화 대기 단계를 추가하십시오. scenario seed는 앞서 설명한 조건에서 애플리케이션 sampling과 순서를 재현하지만, 커널의 packet impairment나 네트워크 타이밍까지 동일하게 재현하지는 않습니다. HopWave는 지원하지 않습니다.

종료된 노드의 이력은 현재 메모리에 유지되며 Agent heartbeat와 Controller snapshot에도 포함됩니다. 장기간 대규모 churn을 실행할 때 control-plane 상태와 payload가 계속 증가하지 않도록 제한된 보존 정책과 별도의 pagination 기반 history API가 추가로 필요합니다.
