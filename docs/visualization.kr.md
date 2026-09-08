# 저장 결과 시각화와 비교

[English](visualization.md) | 한국어

Dashboard의 **Saved results → Analyze** 또는 상단 **Analysis**에서 분석을 엽니다. **Saved run → Add run**으로 최대 4개 실행을 추가할 수 있으며, 첫 실행을 기준으로 평균·P95 지연과 관측 중복 수의 차이를 표시합니다. **Remove**로 선택에서 제외하고 **Refresh analysis**로 선택한 실행을 다시 분석합니다. 실행 중 결과는 요청 시점의 스냅샷이며 자동 갱신하지 않습니다.

잘못된 차트 데이터나 요청 실패가 발생하면 기존 정상 스냅샷과 그 분석 시각을 유지하고 오류를 표시합니다. **Refresh analysis**로 재시도할 수 있으며 실패를 새 측정값 0으로 바꾸지 않습니다.

## 그래프와 필터

| 그래프 | 의미 |
|---|---|
| Propagation latency CDF | 유효한 첫 원격 수신 지연 표본의 누적 비율. 전체 노드 도달률과 다름 |
| Latency / duplicate trade-off | 실행별 평균 지연과 유효 수신 쌍당 관측 중복 수 비교 |
| Latency histogram | 유효 지연 표본의 분포. x축은 구간 중심(ms), y축은 표본 수 |
| Message event rate | 저장된 publish/deliver/duplicate 이벤트 수를 구간 초로 나눈 값 |
| GossipSub control rate | IHAVE/IWANT/IDONTWANT/GRAFT/PRUNE 이벤트를 send/recv/drop으로 합산. 하나의 wire RPC에 여러 종류가 담길 수 있음 |
| P2P stream throughput / Cumulative P2P stream transfer | 실제 보고된 송수신 사용량(kbit/s, KiB), negotiated protocol 선택 가능 |
| GossipSub control breakdown (표) | Agent/방향/control type별 RPC·entry·message ID·peer exchange 수 |
| Peer lifecycle | 그룹별 ready/starting/stopping/failed 상태와 신선한 보고를 가진 Peer 수 |
| Average degree / Clustering coefficient | 선택한 관계 계층에서 평균 고유 이웃 수와 평균 local clustering |
| Degree distribution | 슬라이더로 선택한 관측 시점의 차수별 Peer 비율 |
| Peer score / Negative peer scores | 보고된 observer-to-peer score의 평균·최솟값·최댓값과 음수 비율 |

**Inspect run**은 상세 그래프의 실행을 선택합니다. **Observation group**과 **Relationship layer**는 토폴로지·점수 관측 그래프에 적용하며, 메시지·지연 그래프는 실행 전체를 사용합니다. 점수에는 관계 계층 필터가 적용되지 않습니다. 토폴로지 차수는 해당 실행의 신선한 보고를 가진 노드 사이에서 계산하고 여러 topic에 걸친 동일 이웃을 한 번만 셉니다. 그룹 필터는 통계를 낼 노드를 고르며 다른 그룹으로 향하는 이웃도 차수에 포함합니다. clustering에서 차수 0 또는 1인 노드의 계수는 0입니다.

결과 표는 안정 대상의 전달률과 함께 발행 시점 대상 전달률·안정 coverage 범위·이탈 쌍 수를 표시합니다. 결과 표에서 측정 정의(`definition`)와 `deliveryWindows`, 유효 표본 수, pending/unknown/invalid 수를 함께 확인하십시오. v3의 기존 측정 집계기를 재사용하므로 세션 기간 기반 실행은 그 기간 동안 구독한 안정 수신 대상과 기한 내 수신만 반영합니다. 이전 정의의 로그는 기존 정의를 유지합니다. raw 수신, 무효 지연, 관측 부족은 유효 지연 표본으로 만들지 않습니다. 전달률 범위는 증거에 따른 논리적 상·하한이며 신뢰구간이 아닙니다. 서로 다른 정의나 수신 기간을 쓴 실행의 단순 수치 차이를 같은 조건의 성능 차이로 해석하지 마십시오.

## 기록과 내보내기

Controller는 실행 중인 실험의 관측치를 5초마다 `runs/<id>/observations.jsonl`에 기록합니다. 브라우저 접속 여부와 독립적이며 종료 처리 시에도 마지막 표본을 저장합니다. 이 파일은 결과 ZIP에 함께 포함되고 Controller 재시작 후에도 조회할 수 있습니다. 이전 결과에 관측 파일이 없으면 메시지·지연 분석은 계속 제공하며 과거 토폴로지·점수를 만들어내지 않습니다. 5초보다 짧은 변화나 보고되지 않은 관계는 복원할 수 없습니다.

- **Download analysis JSON**: 현재 선택한 스냅샷들의 집계, 분포, 시계열, 그룹·계층 데이터. 모든 그룹을 포함합니다.
- **Download summary CSV**: 실행별 집계값, 정의, 수신 기간, 분석 시각. 결측값은 빈 셀입니다.
- 각 그래프의 **SVG**: 제목과 범례가 포함된 벡터 이미지.
- **Download results / Download snapshot**: 원본 시나리오·이벤트와 관측 JSONL이 포함된 기존 ZIP.

그림의 반올림 표시는 내보내는 JSON의 원래 수치를 바꾸지 않습니다. 큰 로그의 차트 응답은 시간 구간 최대 360개, 관측 최대 1,440개, CDF 약 200개 지점, 히스토그램 최대 30개 구간으로 축약됩니다. 첫 관측과 마지막 관측은 유지하며, 구간 합계와 전체 표본의 집계는 축약 전 데이터를 기준으로 계산합니다. 정확한 clustering 계산에 필요한 이웃 쌍이 한 계층에서 250,000개를 넘으면 해당 계수는 N/A로 표시합니다. 원본 파일은 축약하지 않습니다.

## 분석 API

```http
GET /api/v1/experiments/{id}/analysis
```

응답은 `version: 1`, `result`, `asOf`, `eventBytes`, `eventCount`, `untimedEvents`, `metrics`, `latencyCDF`, `latencyHistogram`, `timeline`, `binSeconds`, `observations`, `observationCount`, `bandwidthTimeline`, `bandwidthBinSeconds`를 포함합니다. 분포 지점은 `{x, y}`이며 CDF의 x는 ms, y는 0~1 비율입니다. 시간 구간의 이벤트 필드는 구간 합계이며 화면에서만 `binSeconds`로 나눕니다. `asOf`는 파일 경계를 확보한 분석 기준 시각으로, 측정 창의 성숙 여부 판정에도 사용합니다.

404는 없는/삭제된 결과, 405는 잘못된 HTTP 메서드, 422는 읽을 수 없는 기록, 504는 분석 시간 초과입니다. Controller는 한 번에 하나씩 분석하며 대기 시간을 포함해 요청당 2분 제한을 적용합니다. 재시도 이벤트는 이벤트 식별자로 제거하고, 결과 ID와 다른 이벤트는 무시합니다. 최근 300개 이벤트 버퍼와 관계없이 저장된 전체 로그를 읽습니다. 파일 경계를 확보한 뒤 저장 잠금을 해제하므로 분석 중 telemetry 저장을 막지 않습니다.

## v2에서 옮긴 기능

v2의 `kpl-viewer/time_vs_metric_plot.py`(전파 시간 곡선), `basic_graph`(차수 분포·통계), `compare_graph`(기준 실행 대비 차이), `trade-off_graph`(지연·중복 비교)의 분석 목적을 v3 로그와 측정 정의에 맞춰 구현했습니다. v3는 Controller의 Go 분석 API와 내장 JavaScript/SVG를 사용하므로 별도 Python, Matplotlib, CDN, Prometheus 없이 이 화면을 사용할 수 있습니다. v2의 원본 파일 형식을 직접 불러오는 기능이나 특정 실험용 회귀·Pareto 점수 모델은 포함하지 않습니다.

[실험 지표 정의](experiment-metrics.kr.md) · [모니터링과 결과 보존](monitoring.kr.md)

## 대역폭과 v2 전체 대조

[Bandwidth 측정](bandwidth.kr.md)은 실제 libp2p 송수신 사용률·누적량을 저장하고 이 화면과 Grafana에 표시합니다. **GossipSub control breakdown** 표에서 Agent/방향/control type별 RPC·entry·message ID·peer exchange 수를 구분할 수 있습니다.

**v2 전체 기능과 동일하지 않습니다.** [40개 출력 필드와 전체 Python 코드의 대조표](v2-analysis-coverage.kr.md)에서 지원 여부, 다른 정의, 필요한 원자료를 확인하십시오.
