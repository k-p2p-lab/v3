# REST API

[English](api.md) | 한국어

Controller는 아래 공개 및 운영 엔드포인트를 제공합니다. `KPL_API_TOKEN`을 설정했다면 변경 요청에 Bearer 토큰을 보내야 합니다.

## Controller endpoint

| Method | Path | 설명 |
|---|---|---|
| `GET` | `/metrics` | Controller와 실험 metric의 Prometheus exposition |
| `GET` | `/api/v1/health` | Controller 상태와 Peer 시계 측정에 쓰는 현재 UTC 시각 |
| `GET` | `/api/v1/ui-config` | 대시보드 메뉴에 사용하는 Prometheus·Grafana 게시 포트 |
| `GET` | `/api/v1/prometheus/controller-targets` | Controller 공개 metrics 주소의 Prometheus HTTP service-discovery group |
| `GET` | `/api/v1/prometheus/agent-targets` | metrics URL을 알린 online Agent의 Prometheus HTTP service-discovery group |
| `GET` | `/api/v1/snapshot` | 노드 `peerScores`를 포함한 대시보드 전체 snapshot |
| `GET` | `/api/v1/agents` | Agent 상태 |
| `GET` | `/api/v1/nodes` | inspection으로 수집한 `peerScores`를 포함한 Peer 상태 |
| `GET` | `/api/v1/network` | `peerScores`가 포함된 Peer, 연결 edge와 전파 지표 |
| `GET` | `/api/v1/bootstrap?runId={runId}` | 필수 run ID에 속한 준비 상태의 bootstrap peer만 반환 |
| `GET` | `/api/v1/discovery?runId={runId}&topic={topic}&requesterNodeId={nodeId}` | 동일 run·정확한 topic에서 ready·online 상태인 PubSub transport 후보 |
| `GET` | `/api/v1/events` | 최근 trace event |
| `GET` | `/api/v1/stream` | `peerScores`를 포함한 실시간 snapshot SSE |
| `GET` | `/api/v1/experiments` | 실험 상태와 `activeJobs`, `completedJobs`, `failedJobs`, `canceledJobs` counter |
| `GET` / `POST` | `/api/v1/scenarios` | 시나리오 요약 목록 조회 또는 검증한 `{name, yaml}` 저장 |
| `POST` | `/api/v1/scenarios/validate` | 원본 YAML을 저장·실행 없이 검증. 공개 endpoint로 토큰 불필요 |
| `GET` / `PUT` / `DELETE` | `/api/v1/scenarios/{id}` | 저장 시나리오 하나를 불러오기, 갱신 또는 삭제 |
| `GET` | `/api/v1/results` | 이전 Controller 실행에서 저장한 실험을 포함하는 결과 목록 |
| `DELETE` | `/api/v1/results/{id}` | 비활성 저장 결과 삭제. 진행 중 배치·다운로드 보호 |
| `GET` | `/api/v1/experiments/{id}/download` | 저장된 시나리오·메타데이터·수집 이벤트를 ZIP으로 다운로드 |
| `POST` | `/api/v1/experiments` | YAML 1회 실행 또는 JSON `{scenario, repetitions}`로 1~100회 순차 실행 |
| `POST` | `/api/v1/experiments/{id}/stop` | 실행을 취소한 뒤 제한 시간 내 job 종료와 generation-fenced Peer cleanup 수행 |

`/api/v1/bootstrap`의 `runId` query parameter는 필수입니다. registry는 해당 run에서 준비 상태이고 유효한 identity와 address 정보가 있는 `boot` 노드만 반환하므로 동시에 실행되는 실험끼리 bootstrap peer를 발견하지 않습니다. `/api/v1/discovery`에는 표시된 query 세 개가 모두 필요하며 요청자 자신을 제외하고 실제 전달·mesh 결과가 아닌 설정상 topic 참가자를 반환합니다. `/api/v1/prometheus/agent-targets`는 기본 Swarm Prometheus 설정이 사용하는 읽기 전용 운영 endpoint이며 offline Agent와 유효한 metrics URL이 없는 Agent는 제외합니다.

Bootstrap 응답은 `{nodeId, peerId, addresses}` 항목 배열이며 비어 있으면 `null`입니다. Discovery와 달리 bootstrap은 Agent online 상태로 필터링하지 않습니다. Discovery는 후보가 없을 때 빈 배열을 반환하며 각 항목에는 `subscribed` flag도 포함합니다. 조건에 맞는 전체 후보를 반환하고, 요청하는 Peer가 [토폴로지](topology.kr.md#bootstrap과-topic-discovery)에 설명한 rendezvous 순위, connection budget과 retry를 적용합니다.

`/api/v1/prometheus/controller-targets`는 Controller의 `--metrics-url` (`KPL_CONTROLLER_METRICS_URL`)을 광고하며 미설정 시 `[]`를 반환합니다. 인증 없이 조회하는 읽기 전용 endpoint입니다. URL은 HTTP(S)의 `/metrics` 경로여야 하며 인증정보·query·fragment·loopback/unspecified 주소를 허용하지 않습니다. Swarm helper가 control 노드 주소와 `KPL_HTTP_PORT`로 자동 설정합니다.

## 실행 제출, 중지와 관측

`POST /api/v1/scenarios/validate`는 원본 YAML 본문(`Content-Type: application/yaml`)을 최대 1 MiB까지 받습니다. 유효하면 `200`과 `{valid: true, name, phases}`를 반환합니다. 빈 입력, YAML 문법 오류, 알 수 없는 필드, 잘못된 설정, 여러 YAML 문서는 `400`과 `{error: "…"}`를 반환하며 파서가 제공하는 줄 번호를 보존합니다. 본문 한도 초과는 `413`입니다. 저장·실행과 같은 파서를 사용하지만 기록이나 job을 만들지 않고 토큰도 요구하지 않습니다. 설정 검증이며 Agent 용량이나 실행 시 연결성을 검사하지는 않습니다.

`POST /api/v1/experiments`는 첫 run의 experiment 객체와 `202`를 반환합니다. `{scenario, repetitions}`를 제출하려면 `Content-Type: application/json`을 사용하십시오. `scenario`는 YAML 문자열이며 `repetitions`를 생략하면 기본값은 `1`입니다. 다른 content type은 원시 YAML로 처리합니다. 원시 YAML과 JSON에서 해석한 `scenario` 문자열의 한도는 각각 1 MiB입니다. JSON 요청 본문은 escape를 고려해 `6 * 1 MiB + 64 KiB`까지 허용합니다. 요청 본문 자체가 한도를 넘으면 `413`, 해석한 YAML 문자열이 한도를 넘거나 scenario/repetition이 유효하지 않으면 `400`입니다.

반복 제출은 매 iteration에 별도 run ID와 결과 기록을 예약하고 `batchId`, `iteration`, `repetitions`를 공유합니다. 순차 실행하며 실패하거나 취소된 iteration은 대기 중인 나머지 실행도 취소합니다. 반복 배치가 진행 중일 때 구성원 하나를 중지하면 해당 배치를 취소합니다. `repetitions > 1`에서는 마지막을 포함한 매 iteration이 최종 상태를 기록하기 전에 Peer를 fence하고 제거합니다. 자연스럽게 성공한 단일 실행만 YAML에 `stop-all`이 없을 때 Peer를 남겨둘 수 있습니다.

중지 endpoint는 cleanup 완료 전, 취소 요청을 접수하면 `202`를 반환합니다. `/api/v1/experiments` 또는 snapshot에서 최종 상태를 확인하십시오. 취소 handle이 더 이상 없는 run은 `404`입니다. SSE는 최초 `event: snapshot`, 상태 변경 시 전체 snapshot, 15초마다 keepalive comment를 보내며 event ID 기반 replay는 제공하지 않습니다. `/api/v1/events`는 현재 Controller 상태에서 가장 최근 event 최대 300개를 포함합니다.

저장소 루트에서 실행하는 예시입니다. `control-node:8080`은 `sh scripts/swarm.sh access`가 표시한 Controller 주소로 바꾸고, `sh scripts/swarm.sh credentials`가 표시한 토큰을 `KPL_API_TOKEN`으로 export하십시오.

```bash
curl -X POST http://control-node:8080/api/v1/experiments \
  -H 'Content-Type: application/yaml' \
  -H "Authorization: Bearer ${KPL_API_TOKEN:?Set KPL_API_TOKEN}" \
  --data-binary @examples/smoke.yaml
```

시나리오 라이브러리 endpoint는 재사용할 편집기 입력을 실험 결과와 별도로 저장합니다. 목록에서는 YAML을 제외하고, 개별 GET·POST·PUT 응답에는 포함합니다. UI 사용 순서, payload, 검증 제한과 저장 위치는 [시나리오 라이브러리 안내](scenario-library.kr.md)를 참고하십시오.

원시 이벤트는 `data/runs/<run-id>/events.jsonl`, 실행 입력은 같은 디렉터리의 `scenario.yaml`, 실험 메타데이터는 `experiment.json`에 저장됩니다. Swarm에서는 영구 `controller-data` 볼륨을 `/var/lib/kpl/data`에 마운트하며, 실험별 파일은 그 아래 `runs/<run-id>`에 저장됩니다.

대시보드의 **Download results**로 실험 결과를 ZIP으로 받을 수 있습니다. **Saved results**에는 이전 Controller 실행에서 보존된 결과도 표시되며, **Refresh**로 목록을 다시 읽습니다. 실행 중 실험의 **Download snapshot**은 다운로드 시작 시점까지 저장된 기록을 담습니다. 최근 300개 이벤트 버퍼와 별개로 저장된 전체 이벤트 로그를 내보냅니다. 파일 구성과 수집 한계는 [실험 결과 다운로드](monitoring.kr.md#실험-결과-다운로드)를 참고하십시오.

`DELETE /api/v1/results/{id}`는 삭제 성공 시 `204`, 결과가 없으면 `404`, 실행·배치가 활성 상태이거나 실제 `GET` 다운로드가 결과를 사용 중이면 `409`를 반환합니다. 자동 `HEAD` 크기 계산과 목록 조회는 다운로드 충돌로 처리하지 않습니다.

## 내부 cluster endpoint

이 REST/JSON endpoint는 컴포넌트 사이 통신에 사용합니다. 등록, heartbeat와 전달 event는 **Controller로**, lifecycle 명령은 **Agent로** 전송합니다.

| 호출자 → 서버 | Method | Path | 목적 |
|---|---|---|---|
| Agent → Controller | `POST` | `/api/v1/agents/register` | Agent instance 등록 |
| Agent → Controller | `POST` | `/api/v1/agents/heartbeat` | Agent와 Peer snapshot 보고 |
| Agent → Controller | `POST` | `/api/v1/events/batch` | batch당 최대 5000개 event 전달 |
| Controller → Agent | `GET` | `/api/v1/status` | Agent와 Peer 상태 갱신 |
| Controller → Agent | `POST` | `/api/v1/nodes` | `CreateNodeRequest`로 Peer 생성 |
| Controller → Agent | `DELETE` | `/api/v1/nodes/{nodeId}` | Peer 하나의 종료 요청 |
| Controller → Agent | `POST` | `/api/v1/nodes/{nodeId}/publish` | publish 요청 중계 |
| Peer → Agent | `POST` | `/api/v1/nodes/{nodeId}/status` | 해당 Peer의 최신 상태 보고 |
| Peer → Agent | `POST` | `/api/v1/telemetry` | telemetry batch 제출 |
| Agent → Peer | `GET` / `POST` | `/health` / `/publish` | Peer HTTP API로 readiness 확인 또는 publish |

Agent는 Controller가 cleanup에 사용하는 다음 endpoint도 제공합니다.

| Method | Agent path | 설명 |
|---|---|---|
| `DELETE` | `/api/v1/runs/{runId}/nodes?generation=N` | unsigned `generation`이 필수이며, run fence를 N까지 원자적으로 높이고 이후 generation N 이하의 create를 거부하며 해당 generation의 기존 노드를 종료한 뒤 `202 Accepted`를 반환합니다. |

내부 endpoint는 운영자 API와 별개로 변경될 수 있습니다. 요청과 snapshot의 필드 정의는 [`internal/model/model.go`](../internal/model/model.go), handler는 [`internal/controller/api.go`](../internal/controller/api.go)와 [`internal/agent/api.go`](../internal/agent/api.go)에 있습니다.

## 인증

`KPL_API_TOKEN`은 KPL 변경 API에 쓰는 공통 Bearer 토큰이며 Swarm join token, Docker 권한, Grafana 비밀번호와는 별개입니다. Controller와 모든 Agent에 같은 값을 설정하면 Agent가 Peer에도 전달합니다. Swarm stack에서 필수이며 사용자·역할별 권한 분리는 없습니다.

대시보드의 **Run experiment → API token**에 같은 값을 입력하십시오. 이 창에서 실행·저장·갱신·삭제하면 해당 origin의 브라우저 `localStorage`에 저장하여 이후 변경 요청에 사용하며 자동 만료되지 않습니다. REST 요청에는 `Authorization: Bearer <token>`을 붙입니다. 상태·이벤트·SSE·metrics 등 GET 조회는 토큰 설정 후에도 공개입니다. 상태를 바꾸지 않는 `POST /api/v1/scenarios/validate`도 공개이며 해당 method와 정확한 path에만 적용됩니다. Controller는 HEAD도 인증 검사에서 제외하고 Agent와 Peer는 GET만 제외합니다. 토큰 자체가 HTTP 전송을 암호화하지는 않습니다.

같은 네 가지 job counter가 `/api/v1/snapshot`과 SSE snapshot에도 포함됩니다. 대시보드는 각 run에 이를 표시하므로 Controller 로그를 열지 않아도 실행 중, 성공, 실패, 취소된 background 작업 수를 확인할 수 있습니다.
