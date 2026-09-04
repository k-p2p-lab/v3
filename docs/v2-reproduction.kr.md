[English](v2-reproduction.md) | 한국어

# v2 시나리오·네트워크 실험 재현 범위

검토 기준은 `kpl-v2`의 실제 Controller/Peer 코드와 `exp/exp-2603_churn-02.sh`입니다. 설명 문서와 실행 코드가 다를 때는 실행 코드를 기준으로 삼았습니다. 아래 설정으로 실험의 주요 조건을 재현할 수 있으나, 과거 실행의 패킷 순서나 토폴로지가 동일해지는 것은 아닙니다.

## 컨테이너 격리와 배치

양쪽 모두 피어 하나를 컨테이너 하나로 실행합니다. v2는 Swarm Service, v3는 Agent가 생성하는 Docker 컨테이너입니다. v3 Peer에는 호스트 포트, 호스트 파일시스템, Docker socket을 공유하지 않습니다. 각 Peer의 network namespace에만 qdisc를 설치하며, 필요한 Peer에만 `NET_ADMIN`을 부여합니다. CPU·메모리 제한은 v2에도 없었으며, 호스트 부하까지 격리된다는 의미는 아닙니다.

v2의 parallel join은 한 Worker 호스트를 무작위 선택한 뒤 ServiceCreate 요청을 순차 전송합니다. v3에서 이에 대응하는 설정은 `parallel: true`, `parallelism: 1`, `placement: single-agent`입니다. v2의 sequential join은 노드마다 호스트를 다시 선택하므로 `parallel: false`, `placement: random`을 사용합니다. `agentId`를 지정하면 특정 Agent에 고정할 수 있습니다. 기본 `placement: balanced`는 부하 분산 정책입니다.

기본 Compose의 Agent 두 개는 **같은 Docker 호스트**를 사용합니다. 물리 호스트 간 지연·overlay 실험에는 호스트마다 Agent를 두고 공통 attachable overlay를 구성해야 합니다. Agent capacity 대기, Docker CLI 비용, 실제 머신 부하에 따라 목표 join rate와 실제 시작 rate는 달라집니다. v2의 Swarm 자동 복구와 v3의 종료 상태 기록도 다릅니다.

Swarm 다중 서버용 [배포 구성과 확장성 검토](swarm.kr.md)를 추가했습니다. Swarm은 global Agent를 서버별로 배치하고 Controller가 Peer를 분배합니다. Agent task 재시작은 담당 Peer를 정리하며, v2의 Peer Service 재스케줄링을 그대로 재현하지는 않습니다. Peer 광고 주소·Agent별 직접 접근·삭제 완료까지의 capacity 보존·전체 Agent 모니터링을 반영했습니다.

생성 도중 실험을 취소하더라도 Docker create의 응답을 제한시간 내 기다려 컨테이너 ID를 확보한 뒤 제거합니다. CLI만 먼저 종료하여 daemon에 늦게 생성되는 컨테이너를 남기는 경합을 방지합니다. Docker가 삭제 진행 중이라고 응답하면 실제 소멸 여부를 확인하며, 기본 `jobShutdownTimeout`과 Compose 종료 대기는 `3m`입니다. Docker admission 자체의 deadline 초과나 daemon 장애는 오류로 보고하며, 이후 재시작 시 소유 label로 잔여 컨테이너를 정리합니다.

## 네트워크 조건 대응

| v2 설정/동작 | v3 설정 |
|---|---|
| `eth0` 전체 송신에 netem 적용 | `network.scope: all` (기본 `p2p`는 P2P TCP만 적용) |
| `delay.distribution`에서 피어당 값 하나 추출 | `network.delayDistribution` |
| `delay.deviation` 밀리초, 패킷 jitter | `jitter: 5ms`, `jitterDistribution: uniform` |
| `loss`, `corrupt` | `lossPercent`, `corruptPercent` |
| `reorder.percentage`, `reorder.chance` | `reorderPercent`, `reorderCorrelationPercent` |
| `tbf.rate`, `tbf.burst`, `tbf.latency` | `tbf.rateMbps`, `tbf.burstKbit`, `tbf.latency` |

v2의 `chance`는 실제 tc 명령에서는 재정렬 **상관계수**입니다. `rateMbps` 단독은 netem의 패킷 직렬화 지연이며 TBF와 동등하지 않습니다. TBF와 양수 `rateMbps`는 함께 설정할 수 없습니다. TBF latency는 tc가 지원하는 `1us`~`4294967295us` 범위이며 소수 마이크로초는 올림합니다.

```yaml
network:
  scope: all
  delayDistribution:
    model: normal
    mean: 50ms
    sigma: 10ms
    min: 1ms
  jitter: 5ms
  jitterDistribution: uniform
  lossPercent: 1
  reorderPercent: 2
  reorderCorrelationPercent: 25
  tbf:
    rateMbps: 10
    burstKbit: 64
    latency: 100ms
```

`delayDistribution`는 Peer seed로 한 번 샘플링합니다. 패킷마다 바뀌는 jitter와 별개입니다. `delay`와는 배타적이며, jitter/reorder가 활성화되면 양수 `min`을 지정해야 합니다. v2는 초 단위 샘플을 정수 ms로 잘랐지만 v3는 Go duration 정밀도를 유지합니다. v2의 시간 기반 난수 및 gamma 구현 오류까지 복제하지는 않습니다.

`scope: all`은 제어 API 응답·bootstrap 조회·telemetry도 지연시키거나 유실시킵니다. 큰 손실률에서 `wait-ready`나 발행 HTTP 요청이 실패할 수 있는 것도 실험 조건의 일부입니다. 양쪽 모두 송신 방향 shaping입니다. 두 Peer에 delay를 설정하면 왕복 경로에는 두 송신 지연이 더해집니다. 커널 offload, TCP 재전송, 큐 상태 때문에 application 메시지 손실률이 packet loss 설정값과 같지는 않습니다.

## 참여·이탈·발행 스케줄

| 의미 | 대응/주의점 |
|---|---|
| 순차 interval | 작업 종료 후 다음 작업 전 대기, 마지막 작업 뒤 대기 없음 |
| parallel join/leave | interval 무시 |
| parallel publish | 발행자마다 독립적인 시작 지연, interval 생략 시 1초 |
| publish/leave replicas | 후보 수로 제한, 중복 선택 없음 |
| v2 기본 await=false | YAML에서는 `await: false`를 명시 (v3 기본 true) |
| v2 async for-loop | 서로 다른 job phase로 펼침; `repeat`는 한 job의 순차 반복 |
| churn 중 발행 대상 소멸 | `onError: continue`로 해당 실패만 기록하고 계속 |
| 시나리오 정상 종료 후 전체 Peer 정리 | 마지막 `stop-all`을 명시 |

`onError: continue`는 publish/leave에만 적용하며, 후보 0개도 허용합니다. 작업별 실패는 `phase-operation-failed` 이벤트에 남습니다. 사용자 취소와 phase deadline은 계속 전파되며, 개별 HTTP 요청 timeout은 해당 작업의 실패로 처리합니다. 기본 `fail`은 기존 v3처럼 실패 시 실험을 중단합니다.

Docker Peer lifetime은 v2의 ServiceCreate 반환에 대응하는 **Docker create 성공 시점**부터 시작합니다. config copy, 컨테이너 시작, bootstrap에 걸리는 시간은 lifetime에 포함됩니다. `ready`부터의 온라인 수명이 아닙니다. 노드 metadata의 `lifetime`, `lifetimeBasis`, `containerCreatedAt`, `containerStartedAt`, `readyAt`, `stopRequestedAt`, `stoppedAt`으로 구분할 수 있습니다. 각 시각은 Agent가 해당 상태를 관측한 시각이며 `startedAt`은 생성 요청 수락 시각입니다. 종료는 강제 이탈이며, v2 Peer에도 애플리케이션 수준의 정상 종료 절차가 없었습니다.

[`examples/v2-churn.yaml`](../examples/v2-churn.yaml)은 기존 churn 시나리오의 구조를 작은 규모로 옮기고 네트워크 조건을 추가한 예제입니다. 원래 10,000개 join 요청과 수백 개 동시 Peer 규모의 성능 재현 결과를 의미하지 않습니다.

## 초기 연결과 메시지 크기

Peer는 v2처럼 TCP/Noise/Yamux를 명시적으로 사용합니다. Worker는 bootstrap 후보를 seed로 섞고 첫 연결 성공에서 초기 접속을 끝냅니다. 이후 추가 연결은 DHT/PubSub 동작에 맡깁니다. bootstrap 전체 timeout은 목록 조회와 dial에도 적용됩니다.

v2의 `size`는 PubSub에 전달되는 무작위 바이트 길이입니다. v3에서 `payloadEncoding: raw`를 사용하면 `payloadSize`가 정확히 그 길이입니다. libp2p framing, 서명, TCP/IP 헤더까지 포함한 패킷 크기를 뜻하지는 않습니다. 기본 `envelope`는 전파 지연 계측을 위한 JSON과 base64 때문에 더 큽니다. `topic: '*'`는 선택한 노드가 가진 모든 발행 가능한 topic에 각각 메시지를 발행합니다.

raw 메시지는 SHA-256으로 발행·수신을 연결합니다. 메시지 내부에 시각을 추가하지 않으므로 raw 수신에는 지연값을 제공하지 않으며, 평균/P95 계산에서 제외합니다. raw 트래픽을 사용하는 run은 전용 topic과 네트워크로 분리하십시오. envelope의 run ID 필터를 raw 바이트에는 적용할 수 없습니다.

## 관측과 남은 차이

- `wait-ready`는 초기화/API 준비를 뜻하며 mesh 수렴을 보장하지 않습니다. 예제는 별도의 안정화 대기를 둡니다.
- [dashboard 토폴로지](topology.kr.md)는 현재 Peer 상태를 바탕으로 transport, Kademlia 라우팅 테이블, topic별 GossipSub mesh를 별도로 표시합니다. `TopicPeers`는 구독 Peer 수이며 mesh 차수가 아닙니다. 과거 분석에는 저장된 `graft`/`prune`, `add_peer`/`remove_peer`, `join`/`leave` 이벤트를 함께 사용하고 telemetry 누락을 고려해야 합니다. 결과에는 routing·mesh snapshot의 전체 이력이나 완전한 v2 Parser/RPC 분석기는 포함하지 않습니다.
- interval/lifetime/base delay 샘플은 seed로 재현할 수 있으나 run ID를 포함한 Peer ID, 네트워크 타이밍, 커널 패킷 난수는 동일하지 않습니다. 노드 metadata에 `seed`, `networkRequested`, 실제 `network`를 남기며 Agent의 Peer config 파일에도 실효 설정을 저장합니다.
- 기본 연결 상한 55와 주요 worker DHT/GossipSub 파라미터는 일치합니다. 그러나 v2 custom PubSub fork 경로의 소스가 제공된 디렉터리에 없어 fork 내부까지 동등성을 검증할 수 없습니다. v3는 공식 라이브러리이며 HopWave는 지원 범위 밖입니다.
- dashboard 메시지 지표는 최근 이벤트 버퍼와 독립적으로 누적되지만 Controller 재시작 시 초기화되며 telemetry 누락의 영향을 받습니다. 오프라인 분석에는 `runs/<run-id>/events.jsonl`을 사용하고, v2 지표와 직접 동일시하지 말고 [명시된 churn 대상 집합 정의](experiment-metrics.kr.md)를 적용하십시오.

검토한 핵심 v2 파일: `kpl-controller/internal/handler/event.go`, `internal/docker/docker.go`, `internal/distribution/distribution.go`, `cmd/main.go`, `kpl-peer-app/internal/host/{host,tc}.go`, `internal/dht/dht.go`, `internal/api/publish.go`.

## 검증 기록

2026-09-04, Docker Desktop Linux에서 검증했습니다.

- 전체 `go test -buildvcs=false ./... -timeout 60s` 및 모든 예제 YAML 검증 통과.
- 통합 run `run-20260904T021417Z-581b`: 컨테이너 4개의 서로 다른 network namespace, host mount/port binding 없음 확인.
- P2P 전용 netem→TBF와 전체 송신 netem→TBF 모두 실제 커널에 설치. TBF `10Mbit`, burst `8KB`(64kbit), latency `100ms` 확인.
- 동일 worker 프로파일의 피어별 기본 지연은 `57.008524ms`, `50.467284ms`, `47.92387ms`로 각각 추출. 실제 qdisc에서 loss `1%`, reorder `2%`, correlation `25%` 및 packet drop 카운터 확인.
- Worker 3개에서 topic 두 개에 총 6개 raw 메시지 발행. 모든 PubSub data는 정확히 32바이트, 자기 수신을 포함한 24개 수신 이벤트와 도달률 1 확인. raw 지연은 전부 미측정으로 기록.
- 최종 churn run `run-20260904T022448Z-0f9d`: 약 86초 동안 16개 Peer 참여, 발행 3건·수신 8건. 선택된 Peer의 이탈로 발행 1건이 실패했지만 `phase-operation-failed`로 기록하고 계속 진행하여 정상 완료. 모든 Peer stopped 및 잔여 관리 컨테이너 0개 확인.
- 실제 검증에서 발견한 TBF latency 단위 오류, 삭제 시간초과/삭제 진행 중 응답 처리, 취소된 create의 늦은 생성 경합을 수정하고 재검증했습니다.

이 검증은 설정 적용과 작은 규모의 실제 통신을 확인한 것입니다. TBF 최대 처리량, 다중 물리 호스트 overlay, 수백·수천 Peer의 churn 부하 성능까지 측정한 결과는 아닙니다.
