# 실험 지표, 반복 실행, 저장 결과

[English](experiment-metrics.md) | 한국어

## 같은 시나리오 여러 번 실행

**Run experiment**에서 YAML을 입력하고 **Run** 옆의 **Runs**를 1~100 정수로 지정하십시오. Controller가 모든 회차를 대기열에 등록한 뒤 순서대로 실행하므로 브라우저를 닫아도 계속됩니다. 회차마다 고유 run ID, 별도 결과 디렉터리, 메타데이터의 `batchId`, `iteration`, `repetitions`가 부여됩니다.

2회 이상이면 각 회차의 종료 정책에 따라 백그라운드 작업을 취소하거나 완료까지 기다리고, 해당 회차의 Peer 생성을 차단하고 제거한 뒤 Agent 상태를 갱신해야 다음 회차가 시작됩니다. 실행·정리 실패는 남은 회차를 취소합니다. 실행 중이거나 대기 중인 항목의 **Stop batch**는 현재 회차와 나머지 대기열을 함께 취소합니다. 별도로 제출한 다른 실험은 동시에 실행될 수 있으므로 반복 결과 비교 시 중지해 두십시오.

모든 회차의 YAML은 동일합니다. 명시한 0이 아닌 `seed`는 재사용하고, 0이거나 생략되면 회차별로 새 seed를 생성해 기록합니다. 동일 seed는 표본 추출 입력을 반복하지만 Docker 타이밍, 후보 집합, 네트워크 실행 결과까지 동일하게 만들지는 않습니다. Controller 재시작 시 대기열을 재개하지 않으며 남은 `queued`·`running` 기록은 `interrupted`로 표시합니다.

API는 `/api/v1/experiments`에 `application/json`으로 다음 본문을 POST합니다.

```json
{"scenario":"version: 1\nname: repeat-example\nphases:\n  - action: wait\n    duration: 1s\n","repetitions":3}
```

응답은 첫 번째 실험이며 `/api/v1/snapshot`과 SSE에는 모든 회차가 나타납니다. 기존 YAML 원문 요청은 1회 실행을 유지합니다. 변경 요청에는 설정된 bearer token을 사용합니다.

## Churn 중 도달률

관측 단위는 패킷이나 현재 화면의 Peer 수가 아니라 **(run, topic, message, receiver) 쌍**입니다. 성공적으로 발행되고 `publish` 이벤트가 수집된 메시지만 수신 대상 집합을 갖습니다. 실패한 발행 요청은 작업 실패이며 자동으로 유실 메시지로 계산하지 않습니다. RPC 결과가 불명확해도 실제 발행 후 telemetry가 도착했다면 발행 메시지로 집계할 수 있습니다.

각 발행 RPC 직전에 Controller가 `cohortCapturedAt`과 `targetNodeIds`에 준비 완료·온라인·구독 상태인 원격 Peer ID를 기록합니다. 발행자, 다른 run, 해당 topic 비구독자·relay 전용 Peer, 마지막 보고가 오래된 Agent는 제외합니다. 이는 Controller가 마지막으로 관측한 상태이며 다중 호스트 전체의 원자적 동기화 시점은 아닙니다. `topic: "*"`이면 동일 inventory snapshot에서 topic별 대상을 각각 고정합니다.

메시지 `m`의 고정 대상 집합을 `C_m`, 이 중 애플리케이션 수신이 수집된 집합을 `D_m`이라 하면 다음과 같습니다.

```text
expectedDeliveries = 모든 메시지의 |C_m| 합
eligibleDeliveries = 모든 메시지의 |D_m| 합
도달률 = eligibleDeliveries / expectedDeliveries
```

메시지별 백분율을 단순 평균하지 않고 수신 대상 쌍으로 가중합니다. 대상 1명 중 1명 도달, 다음 메시지는 3명 중 1명 도달이면 `2 / 4 = 50%`이며 66.7%가 아닙니다. 대상에 포함된 후 이탈한 Peer는 수신하지 못한 대상으로 남습니다. 토폴로지 원을 지워도 도달률이 높아지지 않습니다. 새 join은 이미 발행된 메시지의 대상에서 제외합니다. 대상이 없으면 0%나 100% 대신 **N/A**입니다.

관측 구간은 실험 시작부터 현재 snapshot 또는 ZIP 경계까지 수집된 기록입니다. 메시지별 고정 수신 마감 시간이나 영구 유실 판정은 구현하지 않습니다. 실행 중 미도달 쌍에는 전파 중·단절·이탈·telemetry 누락이 섞일 수 있습니다. 실험 비교 시 warm-up, 발행, 후속 수집 시간을 동일하게 유지하십시오. churn 예제는 마지막 발행 회차 후 30초를 기다립니다. 완료 후 늦게 도착한 telemetry로 값이 갱신될 수도 있습니다. 이 도달률은 netem의 패킷 손실 확률이나 수신할 때까지 살아남은 Peer만을 조건으로 한 도달 확률과 다릅니다.

## 첫 수신 도달시간

성공한 대상 쌍마다 첫 `Subscription.Next` 성공 시각에서 발행자가 로컬 발행 잠금을 획득한 뒤 해당 메시지를 준비하며 envelope에 넣은 시각을 뺍니다. 직렬화, PubSub 처리, 네트워크, 수신 애플리케이션 큐 지연을 포함합니다. Controller→Agent 전달 시간과 발행자의 로컬 발행 잠금 대기는 제외합니다.

같은 쌍은 가장 이른 애플리케이션 수신만 사용합니다. 발행자 로컬 수신, 이후 중복 전달, 늦은 join, 대상 집합을 모르는 메시지는 제외합니다. raw에는 애플리케이션 발행 시각이 없어 도달률은 계산하되 지연은 **N/A**입니다. 음수 시계 편차 표본은 제외하고 `invalidLatencySamples`에 집계합니다. 양수 방향 시계 오차는 이 방법으로 검출할 수 없으므로 chrony/NTP로 모든 호스트를 동기화하십시오.

UI와 `metrics.json`은 유효한 성공 쌍의 산술평균, nearest-rank P95, `latencySamples`를 제공합니다. 미도달을 0ms로 넣거나 지연 분포에 섞지 않습니다. 빠르게 도달한 일부 Peer만으로 신뢰성을 판단하지 않도록 반드시 도달률도 함께 보고하십시오.

## 평균 중복 메시지

추가 복사본은 수신 측 GossipSub `RawTracer.DuplicateMessage` 관측 1건입니다. PubSub 메시지 캐시에서 이미 본 메시지가 다시 도착한 경우이며 TCP 재전송, 반복 IHAVE, 중복 바이트 수, 두 번째 애플리케이션 전달을 의미하지 않습니다. 성공한 대상 수신자에서 관측된 추가 복사본을 `dup(m,r)`라 하면 다음과 같습니다.

```text
eligibleDuplicates = 성공한 모든 대상 쌍에서 관측한 추가 복사본 합
averageDuplicates = eligibleDuplicates / eligibleDeliveries
```

성공 수신 후 추가 복사본이 없는 쌍은 0으로 포함하고, 성공 쌍 자체가 없으면 **N/A**입니다. 발행자에게 돌아온 복사본과 늦게 join한 Peer의 복사본은 전체 `duplicates` 이벤트 수에 남지만 이 평균에서는 제외합니다. 성공 쌍 3개의 추가 복사본이 각각 0, 1, 5이면 `6 / 3 = 2`개/성공 수신입니다. 바이트 오버헤드 비율이나 발행당 중복 수와는 다릅니다.

envelope의 publish/deliver/duplicate는 동일한 애플리케이션 메시지 ID로 연결합니다. raw는 `pubsub-<원래 메시지 ID의 hex>`를 사용하여 바이트가 동일해도 개별 발행을 구분합니다. 두 형식 모두 `fields.pubsubMessageId`에 원래 ID를 보존합니다. wire 형식과 PubSub의 발신자+sequence ID 계산은 유지합니다. 새 이벤트의 `eventId`는 한 번 생성해 재전송에서도 유지하며 Controller가 한 번만 저장·집계합니다. ID가 없는 과거 이벤트의 전송 재시도는 확실하게 구분할 수 없습니다.

## 집계 범위, 내보내기, 모니터링

새 지표 요약은 `definition: "dispatch-cohort-v1"`로 적용한 정의를 식별합니다.

- 웹 카드는 가장 최근에 시작한 실행 중 실험을 선택하며, 실행 중 실험이 없으면 가장 최근에 시작한 종료 실험을 표시합니다. 대기 회차가 이를 덮어쓰지 않으며 카드 위에 선택한 run을 표시합니다.
- 지표는 현재 Controller 프로세스에서 관측한 실험 전체를 집계합니다. 최근 300개 이벤트와 화면의 40개 행 제한은 이벤트 표시만 제한합니다. 집계용 인덱스는 메시지·수신 쌍·이벤트 ID에 비례해 증가하며 결과 삭제 또는 프로세스 재시작 때 해제됩니다.
- `published`, `delivered`, `duplicates`는 전체 이벤트 집계를 유지합니다. `delivered`에는 로컬·늦은 join 수신도 포함되므로 도달률에는 `eligibleDeliveries / expectedDeliveries`를 사용하십시오. 이벤트 수는 패킷 수가 아닙니다.
- ZIP에는 같은 경계의 `events.jsonl`에서 다시 계산한 **`metrics.json`**을 추가합니다. Controller 재시작 후에도 재계산하며 이벤트 ID로 재시도를 제거합니다. 대상 ID가 없는 과거 메시지는 `unscopedPublications`로 표시하고 도달률·지연·중복 평균에서는 제외합니다. 과거 `targetNodes` 숫자만으로 대상을 추정하지 않습니다. telemetry 누락은 복구할 수 없고 로그가 손상되면 수치를 만들어 내지 않고 ZIP 전송을 실패시킵니다.
- Prometheus는 run별 `kpl_delivery_expected_pairs`, `kpl_delivery_reached_pairs`, `kpl_delivery_ratio`, `kpl_delivery_duplicate_copies`, `kpl_delivery_duplicates_per_reached_pair`를 제공합니다. 분모 0인 비율 시계열은 생략합니다. Grafana의 새 대상 집합 패널은 Run 필터만 적용하며 여러 run 선택 시 쌍 수로 가중합니다.
- `kpl_propagation_latency_seconds`도 동일한 원격 성공 쌍을 run·수신 Agent·topic별로 집계합니다. Grafana는 버킷으로 실험 전체 P50/P95/P99를 근사하며 웹 P95는 정확한 nearest rank입니다. 늦은 telemetry로 히스토그램이 보완·정정될 수 있으므로 `rate`/`increase` 대신 직접 조회합니다. 트래픽 이벤트 카운터의 rate 의미는 유지합니다. 업데이트 전 지연에는 로컬 수신이 포함되었으므로 같은 정의로 생성된 run끼리 비교하십시오.
- Peer 큐 유실, Agent/Controller 실패, 종료 전 미전송 기록은 지표에 편향을 만들 수 있습니다. telemetry 유실도 함께 기록하십시오. `scope: all`의 손실은 계측 경로에도 영향을 줄 수 있습니다.

## 토폴로지와 결과 삭제

Agent 번호는 **Agent status**의 **No.** 열과 연결됩니다. 브라우저 localStorage에 ID↔번호를 유지하며 다른 브라우저는 다른 번호를 배정할 수 있습니다. 실제 ID와 hostname은 표·툴팁에서 확인합니다. `stopping`·`stopped` Peer와 연결 edge는 토폴로지에서 제외하고 failed Peer는 문제 상태로 표시합니다. inventory와 이력은 유지합니다.

**Saved results → Delete**에서 실험 이름·ID를 확인하고 삭제를 확정하십시오. 해당 run의 시나리오·메타데이터·이벤트 로그와 실시간 집계 인덱스를 영구 삭제합니다. Peer 종료나 이미 수집된 Prometheus/Grafana 기록 삭제는 수행하지 않습니다. 실행·대기 중 run, 진행 중 배치 구성원, ZIP 다운로드 중 결과는 보호합니다. 과거 interrupted 결과를 지우는 것은 Peer 정리가 아닙니다.

API는 `DELETE /api/v1/results/{id}`이며 204는 삭제 완료, 404는 없음, 409는 사용 중, 401은 설정된 token이 없거나 잘못된 경우입니다. 지연 telemetry가 결과를 다시 만들지 않도록 작은 삭제 표식을 데이터 디렉터리에 보존합니다. 마이그레이션·백업 시 이 표식도 함께 보존하십시오.

## 참고 문헌과 설계 선택

[Vyzovitis 외 GossipSub 논문 §7.3](https://research.protocol.ai/publications/gossipsub-attack-resilient-message-propagation-in-the-filecoin-and-eth2.0-networks/vyzovitis2020a.pdf)과 [GossipSub v1.1 평가 보고서](https://research.protocol.ai/publications/gossipsub-v1.1-evaluation-report/vyzovitis2020.pdf)는 전파 지연 분포·꼬리 지연, 손실, 중복 전달을 별도로 평가합니다. 이 문헌이 여기의 고정 churn 대상 집합을 규정한 것은 아닙니다. 대상 고정, 쌍 가중, 관측 경계, 성공 수신당 중복 정의는 결과를 본 뒤 분모를 바꾸지 않고 이탈 영향을 보존하기 위한 KPL의 명시적 실험 설계입니다.

[공식 PubSub 메시지 식별 명세](https://github.com/libp2p/specs/blob/master/pubsub/README.md#message-identification)는 발신자+sequence ID와 콘텐츠 기반 중복 제거의 차이를 설명합니다. KPL은 프로토콜 동작을 바꾸지 않고 계측 이벤트를 연결합니다.
