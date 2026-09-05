# REST API

[English](api.md) | 한국어

Controller는 아래 공개 및 운영 엔드포인트를 제공합니다. `KPL_API_TOKEN`을 설정했다면 변경 요청에 Bearer 토큰을 보내야 합니다.

| Method | Path | 설명 |
|---|---|---|
| `GET` | `/api/v1/health` | Controller 상태와 Peer 시계 측정에 쓰는 현재 UTC 시각 |
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
| `GET` / `PUT` / `DELETE` | `/api/v1/scenarios/{id}` | 저장 시나리오 하나를 불러오기, 갱신 또는 삭제 |
| `GET` | `/api/v1/results` | 이전 Controller 실행에서 저장한 실험을 포함하는 결과 목록 |
| `DELETE` | `/api/v1/results/{id}` | 비활성 저장 결과 삭제. 진행 중 배치·다운로드 보호 |
| `GET` | `/api/v1/experiments/{id}/download` | 저장된 시나리오·메타데이터·수집 이벤트를 ZIP으로 다운로드 |
| `POST` | `/api/v1/experiments` | YAML 1회 실행 또는 JSON `{scenario, repetitions}`로 1~100회 순차 실행 |
| `POST` | `/api/v1/experiments/{id}/stop` | 실행을 취소한 뒤 제한 시간 내 job 종료와 generation-fenced Peer cleanup 수행 |

`/api/v1/bootstrap`의 `runId` query parameter는 필수입니다. registry는 해당 run에서 준비 상태이고 유효한 identity와 address 정보가 있는 `boot` 노드만 반환하므로 동시에 실행되는 실험끼리 bootstrap peer를 발견하지 않습니다. `/api/v1/discovery`에는 표시된 query 세 개가 모두 필요하며 요청자 자신을 제외하고 실제 전달·mesh 결과가 아닌 설정상 topic 참가자를 반환합니다.

저장소 루트에서 실행하는 예시입니다.

```bash
curl -X POST http://localhost:8080/api/v1/experiments \
  -H 'Content-Type: application/yaml' \
  -H "Authorization: Bearer ${KPL_API_TOKEN:-}" \
  --data-binary @examples/smoke.yaml
```

시나리오 라이브러리 endpoint는 재사용할 편집기 입력을 실험 결과와 별도로 저장합니다. 목록에서는 YAML을 제외하고, 개별 GET·POST·PUT 응답에는 포함합니다. UI 사용 순서, payload, 검증 제한과 저장 위치는 [시나리오 라이브러리 안내](scenario-library.kr.md)를 참고하십시오.

원시 이벤트는 `data/runs/<run-id>/events.jsonl`, 실행 입력은 같은 디렉터리의 `scenario.yaml`, 실험 메타데이터는 `experiment.json`에 저장됩니다. Compose/Swarm에서는 영구 `controller-data` 볼륨을 `/var/lib/kpl/data`에 마운트하며, 실험별 파일은 그 아래 `runs/<run-id>`에 저장됩니다.

Control Room의 **Download results**로 실험 결과를 ZIP으로 받을 수 있습니다. **Saved results**에는 이전 Controller 실행에서 보존된 결과도 표시되며, **Refresh**로 목록을 다시 읽습니다. 실행 중 실험의 **Download snapshot**은 다운로드 시작 시점까지 저장된 기록을 담습니다. 최근 300개 이벤트 버퍼와 별개로 저장된 전체 이벤트 로그를 내보냅니다. 파일 구성과 수집 한계는 [실험 결과 다운로드](monitoring.kr.md#실험-결과-다운로드)를 참고하십시오.

Agent는 Controller가 cleanup에 사용하는 다음 내부 운영 endpoint를 제공합니다.

| Method | Agent path | 설명 |
|---|---|---|
| `DELETE` | `/api/v1/runs/{runId}/nodes?generation=N` | unsigned `generation`이 필수이며, run fence를 N까지 원자적으로 높이고 이후 generation N 이하의 create를 거부하며 해당 generation의 기존 노드를 종료한 뒤 `202 Accepted`를 반환합니다. |

이 endpoint와 Controller와 Agent 사이의 등록, heartbeat, 노드 lifecycle, publish, batch telemetry endpoint는 REST/JSON을 사용합니다. 이들은 client-facing Controller API가 아닌 내부 cluster·운영 API이며 별개로 변경될 수 있습니다.

`KPL_API_TOKEN`은 KPL 변경 API에 쓰는 공통 Bearer 토큰이며 Swarm join token, Docker 권한, Grafana 비밀번호와는 별개입니다. Controller와 모든 Agent에 같은 값을 설정하면 Agent가 Peer에도 전달합니다. Swarm stack에서는 필수이고 Compose/CLI에서는 선택 사항입니다. 빈 값이면 토큰 검사를 비활성화하며, 사용자·역할별 권한 분리는 없습니다.

대시보드의 **Run experiment → API token**에 같은 값을 입력하십시오. 이 창에서 실행·저장·갱신·삭제하면 해당 origin의 브라우저 `localStorage`에 저장하여 이후 변경 요청에 사용하며 자동 만료되지 않습니다. REST 요청에는 `Authorization: Bearer <token>`을 붙입니다. 상태·이벤트·SSE·metrics 등 GET 조회는 토큰 설정 후에도 공개입니다. Controller는 HEAD도 인증 검사에서 제외하고 Agent와 Peer는 GET만 제외합니다. 토큰 자체가 HTTP 전송을 암호화하지는 않습니다.

같은 네 가지 job counter가 `/api/v1/snapshot`과 SSE snapshot에도 포함됩니다. 대시보드는 각 run에 이를 표시하므로 Controller 로그를 열지 않아도 실행 중, 성공, 실패, 취소된 background 작업 수를 확인할 수 있습니다.
