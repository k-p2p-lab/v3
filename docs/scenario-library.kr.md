# 시나리오 라이브러리

[English](scenario-library.md) | 한국어

대시보드는 재사용할 검증된 YAML 시나리오를 Controller에 저장합니다. 라이브러리와 실험 결과는 서로 독립적입니다. 시나리오 저장만으로 실행되지 않고 편집한 YAML을 실행해도 자동 저장되지 않으며, 어느 한쪽을 삭제해도 다른 쪽은 삭제되지 않습니다.

## 대시보드에서 사용하기

1. **Run experiment**을 엽니다. 현재 Controller에서 **Saved scenarios** 목록을 불러옵니다.
2. 알아보기 쉬운 **Saved scenario name**을 입력하고 YAML을 편집합니다.
3. **Save scenario**를 누릅니다. 새 항목이 선택된 저장 시나리오가 됩니다.
4. 목록의 **Load**를 누르면 해당 이름과 YAML로 편집기 내용을 교체합니다.
5. 이름 또는 YAML을 편집하고 **Save changes**를 누르면 선택한 항목을 갱신합니다. 원본을 유지한 채 새 항목을 만들려면 **Save as new**를 누릅니다.
6. **Delete**와 **Confirm delete**를 차례로 눌러 항목을 삭제합니다. **Refresh**는 Controller에서 목록을 다시 읽습니다.

**New**는 편집기를 기본 예제로 초기화하고 선택한 저장 항목을 해제합니다. 다른 항목을 불러오거나 새 항목을 시작하면 저장하지 않은 편집 내용은 복구 사본 없이 교체됩니다. **Run**은 저장 여부와 관계없이 편집기에 있는 YAML을 실행하며 1~100회 반복할 수 있습니다.

이름은 앞뒤 공백을 제거한 뒤 검사하며 필수이고, Unicode 128자 이하이며 제어 문자를 포함할 수 없습니다. YAML도 필수이며 1 MiB 이하이고 저장 전에 일반 시나리오 검증을 통과해야 합니다. 이름은 중복될 수 있으며 각 항목에는 별도의 생성 ID가 부여됩니다. 목록은 최근 수정 순으로 정렬합니다.

## REST API

요청과 응답 본문은 모두 JSON을 사용합니다. 큰 라이브러리도 가볍게 열 수 있도록 목록 응답에는 YAML을 넣지 않으므로, 편집하기 전에 개별 항목을 조회하십시오.

| Method | Path | 본문 | 성공 응답 |
|---|---|---|---|
| `GET` | `/api/v1/scenarios` | 없음 | `id`, `name`, `createdAt`, `updatedAt` 요약 목록 |
| `POST` | `/api/v1/scenarios` | `{ "name": "…", "yaml": "…" }` | 전체 항목과 `201` |
| `GET` | `/api/v1/scenarios/{id}` | 없음 | `yaml`을 포함한 전체 항목 |
| `PUT` | `/api/v1/scenarios/{id}` | `{ "name": "…", "yaml": "…" }` | 갱신된 전체 항목과 `200` |
| `DELETE` | `/api/v1/scenarios/{id}` | 없음 | 본문 없는 `204` |

`KPL_API_TOKEN`을 설정했다면 `POST`, `PUT`, `DELETE` 요청에 `Authorization: Bearer <token>`이 필요합니다. 현재 Controller 인증 정책에서 조회 요청은 공개입니다. 유효하지 않은 입력은 `400`, 너무 큰 요청은 `413`, 없거나 유효하지 않은 ID는 `404`를 반환합니다.

```sh
curl --fail http://localhost:8080/api/v1/scenarios

curl --fail -X POST http://localhost:8080/api/v1/scenarios \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${KPL_API_TOKEN:-}" \
  --data-binary @- <<'JSON'
{"name":"Smoke baseline","yaml":"version: 2\nname: smoke-baseline\nphases:\n  - action: stop-all\n"}
JSON
```

## 저장과 백업

각 항목은 `<data-dir>/scenarios` 아래의 개별 JSON 파일에 저장됩니다. Compose와 Swarm은 Controller 영구 데이터 볼륨을 `/var/lib/kpl/data`에 마운트하므로 일반적인 서비스 재시작과 `scripts/swarm.sh remove` 후에도 라이브러리를 보존합니다. 시나리오 라이브러리와 실험 결과를 함께 보존하려면 Controller 데이터 디렉터리 전체를 백업하십시오. 라이브러리는 해당 Controller에만 있으며 control node 사이에 복제되지 않습니다.

Controller는 임시 파일과 원자적 파일시스템 연산으로 각 항목을 기록합니다. 목록 또는 항목을 읽을 때 형식이 잘못되거나 예상하지 않은 내용이 있으면 부분적으로 신뢰한 시나리오를 반환하지 않고 오류로 처리합니다.
