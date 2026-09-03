# K-P2PLab v3

K-P2PLab v3는 여러 호스트에서 표준 libp2p GossipSub 네트워크를 구성하고, 재현 가능한 churn/publish 시나리오를 실행하며, 상태와 전파 이벤트를 웹에서 실시간 관측하는 실험 플랫폼입니다. HopWave는 이 버전의 범위에서 제외했습니다.

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
    Peer Peer        Peer Peer     libp2p Kademlia + standard GossipSub
```

- **Controller**: Agent와 Peer 상태, bootstrap registry, 시나리오 실행, 이벤트 저장, REST API와 웹 대시보드를 제공합니다.
- **Agent**: 물리/가상 호스트마다 하나씩 실행합니다. Peer 프로세스의 시작·종료·발행을 담당하고 telemetry를 묶어서 Controller로 전달합니다.
- **Peer**: 결정적 seed로 ID를 생성하고 Kademlia와 표준 GossipSub에 참여합니다.
- **Dashboard**: Agent 용량, Peer 준비 상태, 연결 토폴로지, 실험 단계, 전파 지연과 최근 이벤트를 SSE로 갱신합니다.

## 빠른 실행

Go 1.24 이상 또는 Docker Compose가 필요합니다.

```bash
docker compose up --build
```

브라우저에서 `http://localhost:8080`을 열고 **실험 실행**을 누르면 기본 smoke 시나리오를 실행할 수 있습니다. API 보호가 필요하면 실행 전에 환경 변수 `KPL_API_TOKEN`을 설정하고 대시보드 실행 창에도 같은 값을 입력합니다.

```bash
export KPL_API_TOKEN='replace-me'
docker compose up --build
```

로컬 바이너리로 실행하려면 다음 세 프로세스를 사용합니다.

```bash
go build -o bin/kpl ./cmd/kpl
./bin/kpl controller --listen :8080
./bin/kpl agent --id local-a --advertise-url http://127.0.0.1:8090 --controller-url http://127.0.0.1:8080
```

시나리오 검증:

```bash
./bin/kpl validate --scenario examples/smoke.yaml
```

## 시나리오

시나리오는 순서대로 실행되는 YAML phase 목록입니다. 지원 action은 다음과 같습니다.

| Action | 역할 |
|---|---|
| `join` | Agent 용량을 고려하여 boot/worker Peer를 생성합니다. |
| `wait-ready` | 지정 그룹이 목표 준비율에 도달할 때까지 기다립니다. |
| `publish` | 준비된 노드를 무작위로 선택해 메시지를 발행합니다. |
| `leave` | 지정 그룹에서 노드를 선택해 종료합니다. |
| `wait` | 지정 시간 동안 네트워크가 안정화되도록 기다립니다. |
| `stop-all` | 해당 실험이 생성한 모든 Peer를 종료합니다. |

간격과 수명에는 `fixed`, `exponential`, `normal`, `pareto` 분포를 사용할 수 있습니다. 동일한 scenario seed는 동일한 노드 선택과 분포 샘플을 만듭니다. 전체 예시는 [`examples/smoke.yaml`](examples/smoke.yaml)에 있습니다.

## REST API

| Method | Path | 설명 |
|---|---|---|
| `GET` | `/api/v1/snapshot` | 대시보드 전체 snapshot |
| `GET` | `/api/v1/agents` | Agent 상태 |
| `GET` | `/api/v1/nodes` | Peer 상태 |
| `GET` | `/api/v1/network` | Peer와 연결 edge, 전파 지표 |
| `GET` | `/api/v1/events` | 최근 trace event |
| `GET` | `/api/v1/stream` | 실시간 snapshot SSE |
| `POST` | `/api/v1/experiments` | YAML 시나리오 실행 |
| `POST` | `/api/v1/experiments/{id}/stop` | 실행 중단 |

예시:

```bash
curl -X POST http://localhost:8080/api/v1/experiments \
  -H 'Content-Type: application/yaml' \
  --data-binary @examples/smoke.yaml
```

원시 이벤트는 `data/runs/<run-id>/events.jsonl`, 실행 입력은 같은 디렉터리의 `scenario.yaml`에 저장됩니다.

## 측정 주의사항

전파 메시지에는 발행 시각이 포함됩니다. 서로 다른 물리 서버의 지연을 비교하려면 모든 Agent 호스트에 chrony/NTP를 적용해야 하며, 음수 지연이 감지되면 이벤트에 `clockSkewDetected`가 기록됩니다. 현재 기본 Agent 런타임은 여러 Peer 프로세스를 직접 관리하는 scale 모드입니다. 노드별 Linux `netem` 격리가 필요한 실험은 별도 network namespace 또는 container runtime을 Agent에 추가해야 합니다.
