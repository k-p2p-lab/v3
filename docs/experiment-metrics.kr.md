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

## Churn 도달률: session-window-v1

주 지표의 질문은 **고정 수신 기간 전체에 걸쳐 계속 구독한 원격 애플리케이션 세션 중 몇 개가 마감 전에 수신했는가?**입니다. 지속 구독을 조건으로 하므로 최초 대상 모두가 churn을 견디고 수신할 확률과는 다릅니다. 발행 시점 대상 도달률, 안정 세션 coverage, 관측 품질을 함께 확인하십시오.

관측 단위는 **(run, topic, message, 수신 세션) 쌍**입니다. 같은 node ID를 재사용해도 프로세스 재시작은 새 세션입니다. 성공 발행의 `publish` 이벤트가 수집된 메시지만 분석합니다. 요청 실패는 작업 실패이며, RPC 응답이 불명확해도 실제 발행 후 telemetry가 도착했다면 성공 발행을 관측할 수 있습니다.

### 고정 기간과 구독 증거

`publish` phase의 `deliveryWindow`는 0보다 크고 1시간 이하인 Go duration이며 기본값은 `10s`입니다. 실험 전에 지정하고 비교하는 실험에는 같은 값을 사용하십시오.

`t`는 발행자가 로컬 발행 잠금을 획득한 뒤 기록한 실제 애플리케이션 발행 시각이며, 마감은 `d = t + deliveryWindow`입니다. Controller 요청 시각이나 현재 화면의 Peer 수로 대상을 정하지 않습니다.

Peer는 실제 구독 설정을 완료한 뒤 `measurement_start`에 `sessionId`와 정확한 `fields.subscribedTopics`를 기록합니다. 같은 프로세스에서 2초마다 `measurement_checkpoint`를 보내며 정상적인 세션 종료 때 `measurement_stop`을 기록합니다. 이는 계측된 애플리케이션 세션의 증거이며 물리적 uptime을 측정하는 장치가 아닙니다. checkpoint 주기는 수신 마감의 유예 시간이 아닙니다.

메시지마다 동일 run·topic의 기록된 세션에서 발행자를 제외하고 다음 집합을 재구성합니다.

- **확인된 발행 시점 대상 K:** `t` 이전 또는 같은 시각에 구독을 시작하고, checkpoint 또는 stop이 동일 세션의 `t`까지 지속을 증명합니다. `t` 이후 시작하거나 `t` 이전·같은 시각에 종료가 확인된 세션은 제외합니다.
- **안정 대상 S:** K 중 `d` 이후 또는 같은 시각의 checkpoint/stop이 있으며 `d` 전에는 구독 세션이 종료되지 않은 대상입니다.
- **이탈 D:** K 중 `d` 전에 stop이 기록된 세션입니다. **떠나기 전에 수신했더라도** 안정 대상의 분자와 분모에서 모두 제외합니다. 확인된 발행 시점 대상에는 남습니다.
- **지속 여부 불명 C:** K에 속하지만 `d` 전 이탈과 `d`까지의 지속 어느 쪽도 로그로 증명하지 못한 대상입니다.
- **발행 시점 가용성 불명:** `t` 전에 시작한 후보 세션이지만 `t`에도 존재했는지 기록으로 증명하지 못한 대상입니다. K 밖에 두고 관측 품질로 별도 보고합니다.

확인된 발행 시점 쌍에는 위 세 가지 지속 결과 중 정확히 하나가 대응하므로 `K = S + D + C`가 성립합니다. 강제 종료는 두 종류의 불명 꼬리 구간을 남길 수 있으며, 이를 이탈이나 수신 실패로 자동 분류하지 않습니다.

Agent는 프로세스·컨테이너 종료를 확인하면 `measurement_terminated`도 기록할 수 있습니다. Controller는 이 Agent 이벤트를 받을 때 Agent 호스트의 시각을 Controller 수신 시각으로 바꾸고 원래 값은 `fields.sourceTimestamp`에 보존합니다. 이 수신 시각은 호스트 간 비교 가능한 **종료 시각의 상한**이며 정확한 사망 시각이나 수신 checkpoint가 아닙니다. 발행 전 종료가 확실한 세션을 제외하거나, 별도 Peer checkpoint로 발행 시 생존이 이미 증명된 경우 마감 전 이탈을 확정할 수 있습니다. 불명 꼬리 구간이 마감까지 살아 있었음을 증명하지는 못합니다. Docker churn은 강제 제거를 유지하며 강제 종료는 Peer의 정상 종료 telemetry drain을 실행하지 않습니다.

Transport 단절, GRAFT/PRUNE, mesh 제거, 오래된 inventory, offline Agent는 구독 세션의 stop 증거가 아닙니다. 토폴로지에서 원을 지워도 지표가 좋아지지 않습니다. 이후 도착한 동일 세션의 증거는 이전의 불명 분류를 정정할 수 있습니다.

구독 기간은 `[t, d)`입니다. 정확히 `d`에서 종료한 세션은 해당 기간 동안 계속 구독한 것으로 보고, 정확히 `d`에 수신한 메시지는 기한 내 성공입니다. `t`에서 이미 종료한 세션은 발행 당시 대상에서 제외합니다.

### 기간 완료, 수신, 관측 불명

`d`가 지나지 않은 메시지는 `pending`이며 확정 대상 쌍 집계에서 제외합니다. 기간이 지난 메시지는 해당 수신 세션에서 `d` 이전 또는 같은 시각에 처음 관측된 애플리케이션 수신을 성공으로 처리합니다. 마감 후 수신은 late이며 기한 내 성공은 아닙니다. 발행자 로컬 수신과 늦은 join은 포함하지 않습니다.

Peer 이벤트에는 원본 `sessionId`, 증가하는 `sequence`, 재시도에도 유지되는 `eventId`가 있습니다. 수신 기록이 없을 때는 `measurement_start`부터 해당 기간을 덮는 세션 증거까지 원본 sequence prefix가 연속인 경우에만 확인된 미도달로 처리합니다. 중간 번호 누락이나 잘못된 시각 순서가 있으면 수신 여부는 **unknown**입니다. 재시도로 간격이 채워질 수 있습니다. 마감 경과는 telemetry 수집 완료를 뜻하지 않으므로 확정 집계도 늦은 배치로 정정될 수 있습니다.

확인된 안정 쌍 수를 S, 기한 내 성공을 R, 수신 여부 불명을 U, 확인된 미도달을 F라 하면 다음과 같습니다.

```text
S = R + U + F
안정 대상 도달률 하한 = R / S
안정 대상 도달률 상한 = (R + U) / S
```

U가 0이면 상·하한이 같습니다. 이는 누락된 관측으로부터 계산한 논리적 범위이며 **통계적 신뢰구간이 아닙니다**. 분모가 0이면 N/A입니다. 비율을 높이려고 unknown을 분모에서 제거하지 않습니다. 가용성 불명은 별개 문제입니다. 이 범위는 확인된 안정 세션에 대한 값이며 미지의 전체 모집단까지 설명하지 않습니다.

확인된 발행 시점 대상 도달률은 이탈과 지속 여부 불명 쌍을 분모에 유지하며 이탈 세션의 기한 내 수신도 분자에 포함합니다. K의 기한 내 성공을 Rk, 수신 여부 불명을 Uk라 하면 표시 범위는 `Rk / K`부터 `(Rk + Uk) / K`까지입니다.

안정 coverage도 다음 논리적 범위로 표시합니다.

```text
안정 coverage 하한 = S / K
안정 coverage 상한 = (S + C) / K
```

두 보조 지표는 `K > 0`이면 제공하며, 확인된 발행 시점 대상이 하나도 없을 때만 **N/A**입니다. 발행 시점 가용성 불명 쌍은 K 밖에 두고 `measurementIncomplete`와 함께 관측 품질 경고로 계속 표시합니다. 이 값들이 0이 아니어도 확보한 증거의 결과를 숨기지 않습니다. 범위는 확인된 집단을 조건으로 하며 보이지 않는 모집단 전체를 설명하지 않습니다. 관측된 sequence stream의 측정 범위를 확인할 수 없으면 `measurementIncomplete`를 설정하지만, 아예 관측되지 않은 세션이나 stream은 수신 로그만으로 발견할 수 없습니다. 따라서 false도 실제 모든 Peer를 빠짐없이 측정했다는 증명은 아닙니다.

`metrics.json`에서 `publicationAvailabilityUnknownPairs`는 K 밖의 후보 수, `continuityUnknownPairs`는 C, `stableCoverage`는 하한, `stableCoverageUpperBound`는 상한입니다. 호환성을 위해 `availabilityUnknownPairs`는 두 unknown 범주의 합계를 계속 제공합니다.

Controller의 기존 `targetNodeIds` 요청 직전 스냅샷은 추가 누락 점검 자료로만 유지합니다. 목록에 있는 수신자에게 유효한 세션 시작 기록이 전혀 없으면 측정 불완전으로 표시합니다. 그 수신자를 분모에 넣거나 실제 발행 시점의 생존 여부를 결정하는 데 쓰지는 않습니다. 계측 기록과 이 스냅샷 양쪽에 없는 세션은 여전히 보이지 않을 수 있습니다.

예를 들어 처음 구독자 10명의 기록을 모두 확보했습니다. 마감 전에 2명이 떠났고 그중 1명은 떠나기 전에 수신했습니다. 8명은 기간 내내 구독했고 이 중 7명이 제때 수신했습니다. telemetry가 완전하다면 다음과 같습니다.

```text
안정 대상 조건부 도달률 = 7 / 8 = 87.5%
발행 시점 대상 도달률   = 8 / 10 = 80%
안정 coverage          = 8 / 10 = 80%
```

일찍 수신한 이탈자도 안정 대상의 분자와 분모에서 함께 제외합니다. 그 성공만 남기고 수신하지 못한 이탈자를 제거하면 결과에 따라 대상 자격이 달라집니다.

집계는 **수신 대상 쌍 가중**입니다. 기간이 지난 메시지들의 성공 수와 대상 수를 각각 합한 뒤 나누며 메시지별 백분율을 단순 평균하지 않습니다. 대상 1명 중 1명 성공, 다음 메시지는 3명 중 1명 성공이면 `2 / 4 = 50%`이며 66.7%가 아닙니다. 수신 기간을 바꾸면 기한 내 성공뿐 아니라 끝까지 남는 집단도 달라지므로 같은 기간을 비교하고 coverage를 병기하십시오.

### 시계와 수집 한계

발행·구독·checkpoint·stop·수신 시각은 서로 다른 호스트에서 기록됩니다. 각 Peer는 시작할 때 최대 5초 동안 Controller health 표본을 최대 7개 수집합니다. 빠른 실패가 모든 시도를 즉시 소진하지 않도록 실패한 시도 사이를 250ms 띄웁니다. RTT가 가장 작은 표본의 요청 중간점으로 측정 시각을 Controller 시계에 맞춥니다. 이 작업이 실패해도 Peer는 미동기화 상태로 Ready까지 진행하고 5초마다 다시 시도합니다. 동기화된 뒤에는 30초마다 갱신합니다.

성공한 표본의 신뢰 기간은 2분입니다. 계속된 갱신 실패로 이 기간이 지나면 timestamp 연속성을 위해 마지막 offset은 유지하지만 신뢰할 수 있는 시계 metadata는 더 이상 붙이지 않습니다. 이후 성공하면 metadata를 다시 제공합니다. 유효한 표본이 있을 때 이벤트에 `clockBasis`, `clockOffsetMs`, RTT 절반인 `clockUncertaintyMs`를 기록합니다. Envelope는 발행자 uncertainty를 운반하고 수신자는 양쪽 값을 더한 `latencyUncertaintyMs`를 보고합니다.

Controller는 단방향 지연 추정이 0이나 수신 마감 경계를 넘을 때 이 범위를 사용합니다. 약간 음수인 추정도 uncertainty 범위 안에서 인과적으로 가능하고 확실히 기한 내라면 수신 성공으로 인정하되, 음수 값은 지연 histogram에서 제외합니다. 범위 전체가 발행 전이면 무효, 전체가 마감 후면 late, 마감을 걸치면 unknown입니다. 주기적 측정은 Controller에 연결된 동안 drift를 줄이지만 요청 중간점 추정과 일시적 단절이 호스트 시계 동기화를 대체하지는 않습니다. 특히 장시간 churn 실험에서는 모든 호스트의 chrony/NTP를 계속 활성화하십시오. 계측된 세션의 연속성이 물리적 연결의 연속성을 보장하지는 않습니다.

Peer telemetry는 한도가 있는 재시도와 정상 종료 시 drain을 사용합니다. Agent는 큐가 가득 차면 성공 응답 후 배치를 버리는 대신 backpressure를 적용하며 정상 종료 때 Peer 정리를 허용한 뒤 제한 시간 내 최종 drain을 수행합니다. 큐 초과, 종료 유예 소진, 프로세스·Agent·Controller 실패로 관측이 누락될 수 있습니다. sequence는 일부 누락과 확인된 미도달을 구분하지만 데이터를 복구하거나 아예 보이지 않은 세션의 존재를 증명하지 않습니다. `scope: all`은 계측 경로도 훼손할 수 있습니다. 결과와 함께 유실 카운터 및 incomplete/unknown 표시를 보존하십시오.

## 첫 수신 도달시간

기간이 지났고 안정 대상에 속하며 제때 성공한 원격 쌍만 지연 표본에 포함합니다. 첫 `Subscription.Next` 성공 시각에서 발행자가 로컬 발행 잠금 획득 후 준비한 envelope 시각을 뺍니다. 직렬화, PubSub 처리, 네트워크와 수신 애플리케이션 큐를 포함하고 Controller→Agent 전달 및 로컬 발행 잠금 대기는 제외합니다.

raw에는 애플리케이션 발행 시각이 없으므로 세션 기간 도달률에는 포함하되 지연은 **N/A**입니다. 로컬·후속 수신, 이탈·늦은 join, 지연 미측정은 분포에서 제외합니다. uncertainty 범위로 기한 내 수신을 인정한 경우에도 음수 point estimate는 `invalidLatencySamples`에 기록하고 지연 분포에서 제외합니다.

UI와 `metrics.json`은 산술평균, nearest-rank P95, `latencySamples`를 제공합니다. 미수신이나 unknown은 0ms가 아닙니다. 관측된 기한 내 성공과 안정 구독을 조건으로 한 지연이므로 도달률 범위와 coverage를 반드시 함께 보십시오.

## 평균 중복 메시지

추가 복사본은 수신 측 GossipSub `RawTracer.DuplicateMessage`가 관측한 PubSub 메시지 캐시 hit입니다. TCP 재전송, 반복 IHAVE, 바이트 수, 두 번째 애플리케이션 수신을 의미하지 않습니다.

평균은 안정 대상의 기한 내 성공 쌍에서 같은 수신 기간 안에 관측한 추가 복사본 수를 성공 쌍 수로 나눕니다. 복사본이 없는 성공 쌍은 0으로 포함하고 성공 쌍이 없으면 N/A입니다. 로컬·이탈·늦은 join 수신자 및 기간 밖의 복사본은 전체 이벤트 수에는 남지만 평균에서 제외합니다. 성공 쌍의 추가 복사본이 0, 1, 5개이면 평균은 `6 / 3 = 2`입니다. 중복 telemetry 누락은 이 관측 평균도 낮출 수 있습니다.

envelope 이벤트는 애플리케이션 메시지 ID로 연결합니다. raw는 `pubsub-<원래 메시지 ID의 hex>`를 사용하여 같은 바이트를 발행해도 메시지를 구분합니다. `fields.pubsubMessageId`는 원래 ID를 보존합니다. wire 형식과 PubSub의 발신자+sequence 메시지 ID 계산은 유지하며 telemetry의 원본 sequence는 별도 카운터입니다. 이벤트 ID는 재시도에도 유지하여 Controller가 한 번만 저장·집계합니다.

## 집계 범위, 내보내기, 모니터링

새 요약은 `definition: "session-window-v1"`을 사용합니다. 발행은 `fields.measurementDefinition`과 `fields.deliveryWindow`를 기록합니다. 저장된 세션 증거, 발행·수신 시각, 원본 sequence로 같은 계산을 재구성합니다.

- 웹 카드는 가장 최근 시작한 실행 중 실험을, 없다면 가장 최근 시작한 종료 실험을 선택합니다. 대기 회차가 이를 덮어쓰지 않으며 카드 위에 run을 표시합니다.
- 최근 이벤트 300개·표시 행 40개와 독립적으로 관측한 실험 전체를 집계합니다. 인덱스는 세션·메시지·수신 쌍·이벤트 ID에 비례해 증가하며 결과 삭제 또는 프로세스 재시작 때 해제됩니다.
- `published`, `delivered`, `duplicates`는 전체 이벤트 수를 유지합니다. 수신에는 로컬·늦은 join도 포함되므로 수신 수를 발행 수로 나눈 값은 도달률이나 패킷 손실률이 아닙니다.
- ZIP `metrics.json`은 정확히 같은 경계의 `events.jsonl`에서 재계산하며 Controller 재시작 후에도 가능합니다. 기한 경과 판단에는 manifest에 고정한 `exportedAt`을 사용하므로 재현 계산에도 같은 시각을 적용하십시오. `deliveryWindows`에는 관측한 유효 수신 기간 설정들이 기록됩니다. 이후 telemetry는 해당 다운로드에 포함되지 않습니다. 불완전한 관측은 unknown/incomplete로 표시하며 형식이 손상된 이벤트 로그는 수치를 만들지 않고 내보내기를 실패시킵니다.
- Prometheus `kpl_window_*` gauge는 run별 안정·확인된 발행 시점 대상 수, 도달률 범위, coverage 범위, pending, 이탈, 지속 여부 불명, 발행 시점 가용성 불명과 관측 품질을 제공합니다. Grafana 세션 패널에는 Run 필터만 적용하고 여러 run은 수신 쌍으로 가중합니다. 발행 시점 도달률과 coverage는 품질 경고가 0이 아니어서가 아니라 확인된 발행 시점 분모가 0일 때만 N/A입니다.
- `kpl_window_propagation_latency_seconds`는 같은 성공 쌍을 run·수신 Agent·topic으로 구분합니다. Grafana는 실험 전체 histogram 분위수를 근사하며 웹 P95는 정확한 nearest rank입니다. 늦은 telemetry로 gauge와 버킷이 정정될 수 있어 `rate`/`increase` 대신 직접 조회합니다. 트래픽 이벤트 카운터의 rate 의미는 유지합니다.

### 과거 결과

`dispatch-cohort-v1`은 Controller 요청 직전의 ready/online 구독자 ID를 고정하고 이후 이탈을 분모에 유지했으며 메시지별 마감이 없었습니다. 더 오래된 데이터에는 대상 숫자만 있고 수신자 ID가 없을 수 있습니다. 모두 **과거 정의**이며 지속 구독 세션 측정으로 재해석하지 않습니다. 이전 `kpl_delivery_*`/`kpl_propagation_latency_seconds`를 새 기간 지표와 합산하지 마십시오. 원시 이벤트는 보존하고 혼합 로그의 legacy/unscoped 발행 수는 제외된 기록을 표시합니다. 다운로드로 원래 없던 세션 증거를 만들 수 없습니다. 새 Grafana 세션 패널은 과거 결과만 선택하면 표시할 값이 없습니다.

## 토폴로지와 결과 삭제

Agent 번호는 **Agent status**의 **No.** 열과 연결됩니다. 브라우저 localStorage에 ID↔번호를 유지하며 다른 브라우저는 다른 번호를 배정할 수 있습니다. 실제 ID와 hostname은 표·툴팁에서 확인합니다. `stopping`·`stopped` Peer와 연결 edge는 토폴로지에서 제외하고 failed Peer는 문제 상태로 표시합니다. inventory와 이력은 유지합니다.

[토폴로지 안내](topology.kr.md)에서 Kademlia/GossipSub/Transport 체크박스, topic 필터, 확대·이동·선택과 상태 신선도를 확인하십시오. 화면 조작은 프로토콜 동작이나 측정 대상 집합을 변경하지 않습니다.

**Saved results → Delete**에서 실험 이름·ID를 확인하고 삭제를 확정하십시오. 해당 run의 시나리오·메타데이터·이벤트 로그와 실시간 집계 인덱스를 영구 삭제합니다. Peer 종료나 이미 수집된 Prometheus/Grafana 기록 삭제는 수행하지 않습니다. 실행·대기 중 run, 진행 중 배치 구성원, ZIP 다운로드 중 결과는 보호합니다. 과거 interrupted 결과를 지우는 것은 Peer 정리가 아닙니다.

API는 `DELETE /api/v1/results/{id}`이며 204는 삭제 완료, 404는 없음, 409는 사용 중, 401은 설정된 token이 없거나 잘못된 경우입니다. 지연 telemetry가 결과를 다시 만들지 않도록 작은 삭제 표식을 데이터 디렉터리에 보존합니다. 마이그레이션·백업 시 이 표식도 함께 보존하십시오.

## 참고 문헌과 설계 선택

[HyParView 원문 §2.5·§5.2, 인쇄 p.5·9–10](https://www.dpss.inesc-id.pt/~ler/reports/dsn07-leitao.pdf)은 활성 노드 대비 신뢰성을 정의하고 장애 유발 후 전파를 시험합니다. 이는 생존 집단을 평가하는 질문의 예이며 실패한 이탈자만 사후에 제외하는 규칙의 근거가 아닙니다.

[Pongthawornkamol 외 ICAC 2013, §2.2·§3.2.2–3, 인쇄 p.249·251](https://www.usenix.org/system/files/conference/icac13/icac13_pongthawornkamol.pdf)은 관심 이벤트의 마감 내 전달로 신뢰성을 정의하고 이벤트 발생률로 가중합니다. 해당 broker·link 장애 모델은 수신 세션 churn과 다릅니다.

지속 구독 기간, 세션 증거, 쌍 가중, unknown 범위, 성공당 중복 규칙은 위 논문이 강제하는 표준이 아닌 KPL의 명시적 설계 선택입니다. 고정 기간 전체의 지속을 조건으로 하면 오래 남는 세션을 선택하게 되므로 발행 시점 대상 결과와 coverage를 함께 제공합니다. 이 정의는 v2 재현과 독립적입니다.

[공식 PubSub 메시지 식별 명세](https://github.com/libp2p/specs/blob/master/pubsub/README.md#message-identification)는 원래 메시지 ID를 설명합니다. KPL은 해당 프로토콜 동작을 바꾸지 않고 계측을 연결합니다.
