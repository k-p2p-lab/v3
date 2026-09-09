# 실험 결과 이미지

[English](visualization.md) | 한국어

**Saved results → Images**에서 백그라운드 분석을 시작합니다. 서버가 로그 읽기·그래프 지표·전파 경로·분포 적합·저장 단계를 처리하고, 완료 데이터를 브라우저가 흰 배경의 가로 1,600px PNG로 만듭니다. **PNG ↓**, **CSV ↓**, **Download all PNG + CSV (ZIP)**으로 개별 이미지·차트 좌표·전체 묶음을 다운로드합니다. ZIP에는 축·범례·정의를 포함한 차트 JSON도 있습니다. **Download analysis JSON**은 서버에 저장된 전체 분석입니다.

창이나 브라우저를 닫아도 접수한 서버 분석은 계속됩니다. 같은 실행의 중복 요청은 기존 작업을 재사용하며, 한 번에 1개씩 최대 32개 작업을 접수합니다. 읽기 100% 이후에도 그래프 계산과 저장이 남을 수 있습니다. 완료 데이터는 Controller 재시작 후에도 보존됩니다. 진행 중 재시작한 작업은 `interrupted`가 되며 **Retry**로 다시 시작합니다. 네트워크 요청만 실패한 경우 Retry는 기존 작업에 연결합니다. PNG 변환과 비교 수식 계산은 창을 열었을 때 브라우저에서 수행합니다.

**Analyze latest snapshot**은 새 원본 스냅샷을 분석합니다. 분석기 버전이 바뀐 이전 캐시는 Images를 열 때 새 작업으로 갱신합니다. 원본 로그가 없는 정보는 캐시 갱신으로 복원되지 않습니다. 새 분석·재시도·갱신에는 Controller에 설정된 API 토큰이 필요합니다. 실행 삭제 시 분석 작업과 아티팩트도 삭제됩니다.

## 결과별 이미지

- 기존 v3 지연 CDF·히스토그램, 메시지 활동, Peer 점수, lifecycle.
- GossipSub·transport·Kademlia의 노드 수, 평균 차수, v2의 비잎 분모 차수, 직경·평균 최단 경로, clustering, betweenness·PageRank·degree·closeness·eigenvector 중심성, assortativity, modularity, 연결된 노드 쌍의 비율.
- 차수 확률·누적분포와 가중 Student-t 적합.
- 전파·중복 누적곡선, 선형·로그 시간 축과 복합 패널, hop 확률·누적분포, 메시지별 FRT·reachability·DRC, eager/lazy 추정과 미확인 경로.
- IHAVE/IWANT 등의 RPC 수, control entry 수, message-ID 참조 수, GRAFT/PRUNE 전이와 reciprocal 논리 간선 수.
- 전체·프로토콜별 송수신 kbit/s와 누적 KiB. 대역폭은 libp2p 스트림 사용량이며 회선 용량, IP/TCP 헤더·재전송·관리 API 트래픽을 포함하지 않습니다.

**Message images, repeated runs and v2 comparisons**를 펼쳐 메시지를 선택하고 **Message images**를 누르면 해당 메시지의 지연 CDF, hop 분포, 중복 누적곡선, 최초 수신 경로를 만듭니다. 전체 노드 ID·부모·시각·모집단은 분석 JSON에 있습니다. 근거 부족과 정의되지 않는 통계는 N/A 이미지로 표시합니다.

## 반복 실험과 비교

1. **Load saved results**를 누르고 결과를 선택합니다. **Series**는 비교 집단, **Case**는 동일 조건입니다. 동일 Series/Case만 반복 실험으로 묶습니다. 실패한 전달이나 낮은 도달률 표본을 자동 제외하지 않습니다.
2. Metric·X axis·Scatter color를 고릅니다. Baseline series는 같은 Case끼리의 기준 집단, Reference case는 화살표와 D 파라미터 변화의 기준 조건입니다. 생략하면 첫 집단/조건을 사용합니다.
3. 모델이 필요하면 Parameters에 `i=1,f=0.5,p=90` 또는 `dLow=4,d=8,dHigh=12`를 입력합니다. `i1-f0.5-p90`, `4-8-12` 형태의 Case에서는 자동으로 읽습니다. 일반 실행 이름에서 값을 추측하지 않습니다. p는 백분율입니다.
4. **Analyze selected results and generate comparison images**를 누릅니다. 선택된 서버 분석은 창을 닫아도 계속됩니다. 다시 비교할 때 완료 캐시를 사용합니다.

생성 항목은 반복 평균·표본 SD 오차막대, 차이/`target/(target+baseline)`/비율, 범용 산점도, FRT–DRC와 도달률 색상, 기준점 화살표, 2목적/3목적 Pareto, 가중 점수, i/f 그룹 그림, lazy 기여 추정, v2의 중복 회귀식 4개, Student-t 파라미터의 단변량·다변량 이차 회귀, 관측·적합·예측 비교 및 곡선 overlay입니다. Degree predictions에는 `Dlow,D,Dhigh`를 한 줄씩 입력하여 모델 분포를 만듭니다.

반복 1회에는 표본 SD가 없습니다. 비교 오차는 독립 표본의 1차 오차 전파이고, 같은 입력 자체를 비교하면 오차가 0입니다. 회귀는 case 평균에 적합하며 독립 파라미터가 부족하면 계수를 만들지 않습니다. R²는 학습 자료 내 적합도입니다. 가중 점수는 선택 집합의 min–max 값에 의존하고, 변화 없는 차원은 0.5로 둡니다. Lazy 기여 추정에는 동일 조건의 **lazy-off 기준 집단**을 사용해야 합니다. 도달률 차이를 이용한 관측적 추정이며 인과 효과로 단정하지 않습니다.

## v2 자료와 정의

Import에서 v2 `metrics.jsonl`, 전파 tree JSON/JSONL, 중복 시간 map, `x_case/y/yerr`·`x/y` JSON, 숫자 CSV, v3 분석 JSON을 읽습니다. 파일당 최대 32MiB이며 브라우저 안에서 처리합니다. `x_case/y/yerr`를 읽기 전에 해당 Metric을 선택하십시오. CSV overlay는 단위가 같은 파일끼리 사용합니다. Peak-normalized overlay는 각 곡선 최대를 1로 바꾸므로 원래 확률/횟수가 아닙니다. **v2 fixed historical / ER reference images**는 v2 코드에 들어 있던 고정 참고자료이며 선택한 실험의 관측값이 아닙니다.

기존 v3 메인 Metrics의 수신 기간·세션 기반 정의는 유지됩니다. 추가 연구 지표는 별도의 `research` 영역입니다. FRT는 메시지별 비발행 노드의 최초 수신 시간(초)이며 envelope 또는 동기화된 양쪽 시각/같은 Agent 시각 근거를 사용합니다. 모집단은 최초 수신 시점들에 구독 중이던 비발행 노드의 **합집합**입니다. 구독 이력이 없을 때만 dispatch targets를 사용합니다. 전파·중복 누적비율은 같은 메시지–노드 모집단을 사용합니다. `drc_per_node_count`의 분모는 관측 그래프 평균 노드 수이며, `drc_per_target`과 구분합니다.

그래프는 신선한 보고의 무방향 고유 이웃 관계이고 isolate를 포함합니다. 최단 경로는 자기 자신과 도달 불가능한 쌍을 제외합니다. saved snapshot은 동일 가중치입니다. v2의 reciprocal GRAFT 그래프·메시지별 관측 구간과 수치가 항상 같지는 않습니다. 비잎 분모 차수는 v2처럼 **전체 차수 합 / degree > 1인 노드 수**이며 잎 제거 그래프 평균이 아닙니다. Student-t는 관측 확률에 직접 가중 적합하며, df/scale 경계와 수렴 상태를 결과에 기록합니다.

Controller는 현재 보고에서 얻을 수 있는 그래프 간선을 관측 파일에 저장합니다. Peer는 수정하지 않은 upstream libp2p의 trace 메타정보만 사용합니다. IHAVE의 토픽별 메시지 ID, IWANT/IDONTWANT의 메시지 ID, PRUNE의 peer-exchange ID, 데이터 RPC의 메시지 ID·토픽, 구독 변경을 기록합니다. `rpcObservationId`는 같은 **로컬 RPC 콜백**에서 나온 기록을 묶으며 상대 노드의 RPC ID와 같다는 뜻이 아닙니다. `pubsub_reject`는 거부한 메시지 ID와 제공된 사유를 보존합니다. 실제 필드는 [API](api.kr.md#상세-peer-로그)를 참고하십시오.

Eager Push/Lazy Pull은 **메타정보 기반 추정**입니다. 수신 시점의 활성 GRAFT는 eager 후보, 같은 상대·토픽의 IHAVE→IWANT 순서는 lazy 후보입니다. 새 로그는 전달 메시지의 pubsub ID를 우선 대조하고, ID가 없는 과거 로그는 최대 5초 내 시각 연관을 사용합니다. 이 5초는 분석 규칙이며 프로토콜 timeout이 아닙니다. PRUNE은 활성 GRAFT를 해제하며, 연결 해제·구독 해제·측정 종료는 그 이전 연결 근거를 무효화합니다. 두 후보가 충돌하거나 근거가 부족하면 unknown으로 남깁니다. 경로 전체를 복원할 수 없으면 간선의 추정 근거는 보존하되 전체 경로를 분류하지 않습니다. 요청 ID가 일치해도 실제 발신 원인을 직접 측정한 것은 아닙니다.

ID 상세 목록은 종류별 최대 8,192개 및 hex 문자열 합계 512KiB로 제한하고, 생략 수와 완전성 표시를 기록합니다. 집계 개수는 잘리지 않습니다. 구독 목록도 최대 8,192개입니다. payload 본문이나 수집할 수 없는 큐 출처·패킷 헤더를 기록하지 않습니다. 예전 로그의 누락된 ID와 간선은 재분석으로 복원되지 않습니다. **Controller와 Peer 이미지를 새 빌드로 배포한 뒤 실험**하면 새 상세 로그를 사용할 수 있습니다. third_party와 로컬 libp2p fork는 사용하지 않습니다.

API: `POST /api/v1/analysis-jobs/{id}`, 같은 경로의 `GET`, `/result?jobId={jobId}`의 전체 분석, `/summary?jobId={jobId}`의 경량 비교 자료를 사용합니다. 완료 분석의 `analysisVersion`은 3입니다. 동기식 `/api/v1/experiments/{id}/analysis`는 호환용이며 2분 제한을 유지합니다. Python과 별도 이미지 서버는 필요하지 않습니다.

[실험 지표](experiment-metrics.kr.md) · [v2 기능 대조](v2-analysis-coverage.kr.md) · [Bandwidth](bandwidth.kr.md)
