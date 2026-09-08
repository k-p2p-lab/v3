# v2 분석 기능 전체 대조

[English](v2-analysis-coverage.md) | 한국어

**결론: 현재 v3에서 v2의 모든 분석 요소를 동일한 정의와 그림으로 시각화할 수는 없습니다.** 관련 차트가 있는 것과 v2 수치/통계/입력 형식까지 일치하는 것은 다릅니다. 이번 변경은 실제 P2P bandwidth 수집·시각화와 이미 수집된 control 세부 값의 화면 연결을 추가했습니다. 아래 미지원 항목을 완료로 간주하지 않습니다.

2026-09-08, 같은 작업공간의 `v2` (`74b71090410cac1315ae08f479a95c086feeaaf6`)를 확인했습니다. 분석기 출력 **40개 필드/패밀리**, Python **22개 모듈**, 분석기·입력 타입·일괄 실행 스크립트까지 **34개 파일**을 [감사 목록](v2-analysis-coverage.json)에 기록했습니다. `degree_distribution-*`는 가변 차수 키 패밀리 하나로 셉니다. 출력 키 개수는 기능 지원율이 아닙니다.

v3 저장소 루트에서 실행하며 기본 경로 `../v2`에 v2 체크아웃이 필요합니다. 다른 위치라면 경로를 지정하십시오.

```sh
python3 scripts/audit-v2-analysis.py
# v2가 다른 위치라면:
python3 scripts/audit-v2-analysis.py --v2 /path/to/v2
```

이 명령은 원본 출력 키/파일 집합/해시가 검토한 목록과 일치하는지 확인합니다. 새 원본이나 변경된 분석 코드는 재검토 대상으로 보고합니다. **v3 계산 결과의 동등성을 자동으로 증명하는 테스트가 아닙니다.** 화면 구현과 통계 정의를 아래처럼 별도로 대조했습니다. 원본 링크는 검토한 v2 리비전에 고정했으므로 이웃 체크아웃이 없어도 열 수 있습니다.

## 원본 분석기의 모든 출력

근거: [log.go](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-parser/internal/analysis/log.go), [metric.go](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-parser/internal/analysis/metric.go), [value.go](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-parser/internal/analysis/value.go), [propa.go](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-parser/internal/analysis/propa.go).

| v2 출력 | 현재 v3 | 동일성 / 필요한 추가 작업 |
|---|---|---|
| `time` | 이벤트 시각, 관측 시각, 상대 초 축 | v2는 로그 기반 interval 경계, v3는 이벤트 bin/5초 관측. 같은 표본 시점이 아님 |
| `node_count` | Peer lifecycle 및 topology의 eligible nodes | v2 JOIN/LEAVE 그래프 노드 수와 v3 Ready/fresh-report 수는 다른 모집단 |
| `average_degree` | 관계 계층별 평균 고유 이웃 수 | v2는 reciprocal GRAFT/PRUNE 그래프의 `EdgeCount()/Nodes`; v3는 신선한 보고에 기초한 이웃 합집합. 같은 그래프/분모를 재현하지 않음 |
| `average_degree_excluding_leaves` | 미지원 | v2는 원래 그래프 EdgeCount를 degree > 1 노드 수로 나눔(없으면 분모 1). 잎 제거 후 그래프의 평균 차수로 대체하면 안 됨 |
| `diameter`, `shortest_path_length` | 미지원 | 시간별 그래프 및 v2 netkit의 방향성·단절 그래프·경로 평균 규칙 재현 필요 |
| `clustering_coefficient` | 평균 local clustering | v3 그래프/표본이 다르고 계산량 제한 초과는 N/A. v2와 수치 동등성 미보장 |
| `degree_distribution-*` | 선택 시점의 차수 분포 | v2 viewer는 메시지/실행별 평균 빈도를 모아 정규화. v3 순간 분포와 다름 |
| `betweenness_centrality`, `page_rank`, `degree_centrality`, `closeness_centrality`, `eigenvector_centrality` | 미지원 | 관련 그래프 원자료와 동일 정규화/알고리즘 구현 필요 |
| `assortativity`, `modularity` | 미지원 | v2 알고리즘의 단절/무간선 처리와 community 규칙까지 확인해 구현해야 함 |
| `send_ihave`, `send_iwant`, `recv_ihave`, `recv_iwant` | Control rate + control breakdown 표 | RPC에 해당 control type이 나타난 횟수, entry 수를 별도 표시. v2의 이벤트 발생 지점·시간 구간·메시지별 재집계와 같다고 주장하지 않음 |
| `send_graft`, `send_prune` | 같은 control breakdown 표 | v2 그래프 전이 이벤트와 v3 wire control 관측 의미가 다름 |
| `logical_send_ihave`, `logical_send_iwant`, `logical_recv_ihave`, `logical_recv_iwant` | 표의 Message ID references, Grafana control ID 지표 | ID 참조 개수라는 단위는 대응. v2의 per-message interval 통계/케이스별 그림은 아직 없음 |
| `logical_send_graft`, `logical_send_prune` | 미지원 | v2의 reciprocal GRAFT 처리/성공한 edge removal 수. wire entry 개수로 대체할 수 없음 |
| `eager_count`, `lazy_count` | 미지원 | v3 DELIVER 이벤트에 `EAGER_PUSH`/`LAZY_PULL` 원인 표식이 없음. IHAVE/IWANT의 존재만으로 첫 수신 원인을 단정할 수 없음 |
| `frt` | Mean/P95 latency, latency histogram/CDF | v2는 메시지별 DELIVER 지연 통계. v3는 수신 기간 내 stable receiver의 유효 첫 수신; 표본 집합/초↔ms/집계 순서 다름 |
| `reachability` | Stable delivery 범위, starting delivery, coverage | v2는 관측 수신 수 / 해당 메시지 수신 시점들의 그래프 노드 합집합 크기. v3 세션·수신 기간 기반 분모와 다름 |
| `drc` | Raw duplicate 수와 duplicates / eligible delivery | v2 raw 메시지별 중복 합계 및 viewer의 `drc / node_count`와 v3의 중복 평균 분모가 다름 |
| `eager_frt`, `eager_reachability` | 미지원 | eager/lazy 원인 계측을 추가해야 복원 가능 |
| `pub_map`, `propa_map`, `dup_map`, `reach_map` | 관련 원본 publish/deliver/duplicate/세션 이벤트 | v2 map 파일/리듀서 직접 내보내기나 import 없음. 전파/도달 map은 v3 수신 정의와 일치하지 않음 |
| `eager_propa_map`, `eager_reach_map` | 미지원 | eager 전파 원자료 없음 |

`--graphmetrics`가 꺼져도 v2는 diameter를 계산합니다. shortest-path/clustering/중심성/assortativity/modularity는 해당 옵션에서 추가됩니다. v2의 `average/deviation/median/count`는 메시지별 수신 표본 또는 그 메시지가 집계에 포함된 여러 시간 표본에 대해 계산되며, deviation은 모집단 표준편차, median은 정렬 후 `values[len/2]`입니다. v3의 run mean/P95와 표본 단위가 다릅니다.

추가 출력인 **메시지별 전파 트리**(`id/time/children`)와 **발행 후 시각별 중복 수 map**도 대조했습니다. v2는 FromNodeID 부모가 트리에 붙어야 자식을 붙이고, 끝까지 부모를 찾지 못한 수신은 누락합니다. v3에는 PeerID/RemotePeerID와 메시지 상관 정보가 있는 로그가 있지만 트리 복원, hop 분포, 메시지별 duplicate-time 곡선은 구현하지 않았습니다. raw payload나 부모 기록 누락/relay 경로에 대한 별도 정책이 필요합니다. 현재 실시간 토폴로지 그림은 메시지 전파 트리가 아닙니다.

## Python 시각화·파생 분석 전체

같은 디렉터리의 보조 `file_helper/process_helper/math_helper/format_helper/output_helper`는 각 주 모듈 행에 포함합니다. `old/`도 제외하지 않았습니다.

| v2 모듈 | 실제 분석 기능 | 현재 v3 대조 |
|---|---|---|
| [basic_graph](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/basic_graph/main.py) | 임의 x/y 지표·case/d/d_value 선택, 케이스 그룹 mean/std error bar, 차수 분포, JSON/이미지 | 관련 기본 차트는 있음. 임의 축/케이스 그룹/반복 실험 통계/오차막대 없음. 기본 reachability ≤ 0.1 제거 규칙도 옮기지 않음 |
| [compare_graph](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/compare_graph/main.py) | 공통 case 정렬 후 `t-b`, `t/(t+b)`, `t/b`, 각 독립 오차전파 | 첫 실행 대비 mean/P95 latency와 duplicate 평균 차이만 있음. ratio/div/오차전파/케이스 정렬 없음 |
| [trade-off_graph](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/trade-off_graph/main.py) + plot.py | FRT–DRC, reachability 색상, baseline 대비 Δ, 2D 및 3목적 Pareto, 정규화 가중 점수, i/f별 그룹 | 기본 latency–duplicate 산점도만 있음. 3지표 Pareto/점수/색상/케이스 grouping 없음. 원본 `i-f-p` 매개변수와 baseline `i1-f1.0-p100`, 가중치 0.25/0.25/0.5의 명시적 설정 필요 |
| [time_vs_metric_plot.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/time_vs_metric_plot.py) | 전파 누적곡선, 시간별 중복 누적/노드, 결합 그림, log-x·origin 선택·수치 표본 출력 | latency CDF와 event rate만 관련됨. v2 분모는 메시지 수×metrics 평균 node_count로, 수신 latency 표본 수로 나누는 v3 CDF와 다름. duplicate curve/결합/log-x 없음. 지수 fitting 함수는 있지만 호출이 주석 처리되어 기본 출력 기능으로 세지 않음 |
| [calc_lazy_metric.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/calc_lazy_metric.py) | on/off 실험의 ΔR, net_cov_nodes, overlap_est, net_eff, overlap_ratio, lazy_first_ratio, net_cov_ratio, lazy_first_per_iwant 표 | 미지원. case 정렬된 node_count/IWANT/lazy-first/on-off reachability 입력과 동일 분모 필요. 이 계산만으로 인과 효과가 증명되는 것은 아님 |
| [dup_regression.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/dup_regression.py) | i/f/p 기반 네 종류 LinearRegression, 계수·절편·R² 출력 | 미지원. 메타데이터와 명시적 학습 표본/모델 규칙 필요 |
| [old/graphic.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/graphic.py) | 이전 버전 임의 축·색상 산점도 및 데이터 추출 | 고정 지표 차트만 있음 |
| [old/propagation.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/propagation.py) | 트리 기반 메시지별/평균 hop 분포 | 미지원 |
| [old/prop_plot.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/prop_plot.py) | 전파 time/hop PDF·CDF, 메시지별/통합 평균곡선, CSV/PNG | 미지원. v3 latency histogram/CDF는 같은 원자료/분모가 아님 |
| [old/prop_plot_graph.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/prop_plot_graph.py) | CSV 곡선 폴더별/전체 overlay, peak=1 정규화, long-form CSV | 미지원. 현재 run CDF 비교는 CSV import/peak 정규화가 아님 |
| [old/mean_degree_ratio.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/mean_degree_ratio.py) | 레거시 degree Total/Count를 엔트리별 평균 후 줄/파일 전체 평균으로 출력 | 미지원. 현재 parser `average_degree` 형식과도 다른 입력 스키마 |
| [old/clustering_vs_eigenvector.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/clustering_vs_eigenvector.py) | 두 지표 결합 산점도와 기준 case에서 이동 화살표 | 미지원 |
| [old/frt_drc_graph.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/frt_drc_graph.py) | 코드에 고정된 실험값/ER 값 비교, 오차전파와 두 그림 | 미지원. 현재 실행에서 얻을 수 없는 고정 참조 데이터까지 필요 |
| [old/fitting.py](https://github.com/kmu-comnet/kpl-v2/blob/74b71090410cac1315ae08f479a95c086feeaaf6/kpl-viewer/old/fitting.py) | 차수 확률에서 표본 재구성, Student-t fitting, 역자유도와 이차 회귀, 예측 grid | 미지원 |
| image_gen.sh / total_image_gen.sh 및 각 run.sh / run_more.sh | 위 분석기를 특정 경로·case 설정으로 일괄 실행 | v3는 선택 실행 최대 4개, JSON/CSV/SVG export. 기존 배치/경로/PNG 산출물 호환 없음 |

## 남은 이식의 선행 조건

1. **정의 호환 계층**: v2 호환 지표를 별도 정의로 지정하고 graph 방향성, GRAFT/PRUNE 처리, 시간 간격, 메시지별 가중치, 정규화·결측·0분모 규칙을 고정해야 합니다. 이름만 같은 v3 지표로 바꾸면 연구 결과가 달라집니다.
2. **그래프 원자료**: 기존 `observations.jsonl`은 차수 histogram·평균·clustering 등 요약치만 저장합니다. 요약치만으로 과거 diameter/centrality/modularity를 복원할 수 없습니다. 원본 관계 표본을 추가 저장하거나 충분한 trace 이벤트의 별도 replay가 필요합니다.
3. **전파 원인 계측**: eager/lazy 최초 수신 근거와 전달 경로 추적을 Peer에 추가해야 합니다. 기존 기록에 없던 원인을 사후 추측으로 채울 수 없습니다.
4. **반복 실험·연구 모델**: 배치/iteration은 v3에 있지만 지금 화면은 실행별 비교입니다. case 매개변수, 표본 단위, 오류막대 의미, 공통 case 정렬과 모델별 입력을 명시해야 v2 파생 분석을 안전하게 옮길 수 있습니다.

이번 [Bandwidth](bandwidth.kr.md)는 v2의 누락된 차트를 이름만 옮긴 것이 아니라 새로 수집하는 실제 libp2p 스트림 사용량입니다. 위 미지원 항목의 원자료를 대신하지 않습니다.

[현재 시각화 기능](visualization.kr.md) · [실험 지표 정의](experiment-metrics.kr.md)
