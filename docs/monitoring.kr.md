[English](monitoring.md) | 한국어

# Prometheus와 Grafana로 실험 분석하기

`docker compose up -d --build`로 Controller, Agent 두 개, Prometheus, Grafana를 함께 실행합니다. 데이터 소스와 **KP2PLab Experiment Analysis** 대시보드는 자동 등록됩니다.

대시보드는 영어로 작성되어 있으며, Compose/Swarm은 `GF_USERS_DEFAULT_LANGUAGE=en-US`로 Grafana UI 기본 언어를 영어로 설정합니다. 사용자나 조직의 언어 설정이 있으면 해당 UI 기본값보다 우선합니다. [Grafana 언어 설정 문서](https://grafana.com/docs/grafana/latest/administration/organization-preferences/#change-grafana-language)

Grafana는 최초 실행 시 SQLite 데이터베이스를 초기화하므로 디스크 성능에 따라 준비까지 수 분이 걸릴 수 있습니다. `docker compose logs -f grafana`에서 초기화 진행 상황을 확인할 수 있으며, 이후 시작에서는 기존 데이터베이스를 사용합니다.

로컬 SQLite 데이터베이스에는 WAL 모드를 사용합니다. 데이터베이스와 WAL 파일은 같은 Grafana named volume에 보존됩니다. [Grafana 데이터베이스 설정](https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/#wal)

| 화면 | 기본 주소 |
|---|---|
| KPL Control Room | http://localhost:8080 |
| Grafana 실험 분석 | http://localhost:3000/d/kpl-experiments |
| Prometheus 쿼리/수집 상태 | http://localhost:9090 |
| Controller 지표 원문 | http://localhost:8080/metrics |

Grafana는 로컬 분석용 읽기 전용 익명 접속을 허용합니다. 관리자 계정은 `.env`의 `GRAFANA_ADMIN_USER`/`GRAFANA_ADMIN_PASSWORD`를 사용합니다. 이 작업 환경에서는 임의 관리자 비밀번호를 생성하여 Git에서 제외되는 `.env`에 저장했습니다. 신규 설치 시에는 `.env.example`을 참고하여 설정하십시오. `.env` 없이 실행할 경우 관리자 초기 계정은 Grafana 기본 `admin`/`admin`입니다. 이미 생성된 Grafana 데이터 볼륨의 비밀번호는 환경 변수만 바꾸어도 갱신되지 않습니다.

Prometheus/Grafana 포트는 `127.0.0.1`에만 게시합니다. 기본 포트를 변경하려면 `.env`의 `PROMETHEUS_PORT`, `GRAFANA_PORT`를 설정합니다. 익명 열람을 끄려면 `GRAFANA_ANONYMOUS_ENABLED=false`로 지정합니다.

## 실행과 분석

1. Control Room의 **Run experiment**에서 [`examples/monitoring.yaml`](../examples/monitoring.yaml)을 실행합니다. envelope 발행과 raw 발행을 함께 확인하는 작은 실험입니다.
2. Grafana에서 **Run**(run_id), **Agent**, **Topic**을 선택합니다. 여러 run을 고르면 선택한 실험의 트래픽·지연을 합산하며, 네트워크 설정 시계열은 범례에서 run별로 구분합니다.
3. 실험이 끝난 뒤에도 시간 범위를 해당 실행 구간으로 지정하면 시계열을 볼 수 있습니다. 기본 새로고침은 5초입니다.

Prometheus는 Controller와 두 Agent의 `/metrics`를 5초마다 수집합니다. Peer를 직접 scrape하거나 각 컨테이너에 exporter를 추가하지 않습니다. Controller는 기존 telemetry를 누적 집계하므로 `scope: all`에서 telemetry가 손실되면 지표도 영향을 받습니다. 모니터링 서비스는 별도 Docker network에 있으며 Peer에는 추가 네트워크·권한을 부여하지 않습니다.

## 실험 결과 다운로드

Control Room의 실험 항목이나 **Saved results**에서 **Download results**를 선택합니다. 저장 목록은 Controller의 데이터 디렉터리를 읽으므로 Controller 재시작 후에도 결과에 접근할 수 있습니다. 백업 파일을 복원한 뒤에는 **Refresh**로 목록을 갱신합니다. 실행 중 실험에는 **Download snapshot**이 표시됩니다. 비활성 행의 ZIP 크기를 아직 모르면 UI가 백그라운드에서 측정한 뒤 표시합니다. 파일이 자주 바뀌는 실행·대기 중 행은 크기를 생략합니다. 이후 이벤트가 도착하거나 delivery deadline이 지나면 다음 snapshot과 크기가 달라질 수 있습니다.

ZIP에는 다음 파일이 들어 있습니다.

| 파일 | 내용 |
|---|---|
| `scenario.yaml` | 실험 실행 시 제출한 시나리오 원문 |
| `experiment.json` | 저장된 실험 메타데이터·상태·seed·job 카운터 원문 |
| `events.jsonl` | 내보내기 기준 시점까지 저장된 전체 이벤트. 한 줄에 JSON 하나이며, 기록된 이벤트가 없으면 빈 파일 |
| `metrics.json` | 동일 이벤트 로그 경계에서 재계산한 세션 기간 도달률 범위, 발행 시점 대상 결과, coverage, pending/unknown, 첫 원격 지연, 관측 중복 수. 과거 정의는 legacy 유지 |
| `export.json` | 내보내기 시각, 실험 상태, active/partial 여부와 원본 파일의 캡처된 크기 |

Controller의 저장 잠금 안에서 파일 크기를 확보한 뒤 잠금을 해제하고 ZIP을 전송합니다. 이후 추가된 이벤트는 제외되며 느린 다운로드가 telemetry 파일 쓰기를 붙잡지 않습니다. 완료된 실험에도 지연된 telemetry가 도착할 수 있으므로 나중에 추가된 기록까지 필요하면 수집이 안정된 뒤 다시 다운로드하십시오. `partial: false`는 기록된 실험 상태가 종료 상태라는 의미이며 telemetry 무손실을 보장하지 않습니다. 메시지 본문, PCAP, Prometheus/Grafana 데이터베이스는 ZIP에 포함하지 않습니다.

결과 목록 요청은 메타데이터를 읽고 파일 identity와 크기를 캡처한 뒤, 정확히 같은 파일·상태 버전에 대해 이전에 준비한 `downloadBytes`가 아직 유효할 때만 반환합니다. Cache miss에서는 ZIP을 만들지 않습니다. UI는 값이 없으면 `HEAD /api/v1/experiments/RUN_ID/download`로 준비합니다. HEAD는 실제 다운로드와 같은 스트리밍 ZIP encoder를 바이트 counter에 실행하고 본문 없이 정확한 `Content-Length`를 반환합니다. 이어지는 GET은 준비한 측정을 사용해 archive를 전송합니다. ZIP 자체는 메모리나 디스크에 보관하지 않습니다. 같은 run의 동시 cache miss는 한 번의 측정을 공유하며 서로 다른 run은 최대 두 개까지만 동시에 측정합니다.

Pending publication이 없으면 파일과 상태가 같은 동안 측정한 바이트 수를 유지하면서 HEAD 또는 다운로드마다 정밀도를 유지한 새 export 시각을 기록합니다. 저장 방식이 고정된 `export.json`에는 고정 폭 시각 문자열을 사용하므로 시각이 바뀌어도 인코딩 길이는 같습니다. Stable 목록 행에는 `downloadSizeMaxAgeMs`가 없고 stable HEAD 응답에는 `X-KPL-Result-Size-Max-Age-Ms`가 없습니다. Pending publication이 있으면 바이트 수와 export 경계를 짧게 함께 재사용합니다. 목록 행은 `downloadSizeMaxAgeMs`를 포함하고 HEAD는 `X-KPL-Result-Size-Max-Age-Ms`를 제공합니다. 두 값은 서버의 남은 cache 유효 시간에서 응답 안전 여유 1초를 뺀 양의 밀리초이므로 client와 server의 시계가 일치하지 않아도 됩니다. 준비된 값을 처음 확인한 목록만 유효 시간을 한 번 연장하고, 연장한 경계에서 계산한 max age를 응답하므로 바로 이어지는 GET이 표시 크기와 일치합니다. 목록을 반복해서 새로고침해도 deadline이 계속 밀리지는 않습니다. 안전 여유를 제외하고 남은 유효 시간이 없으면 목록은 `downloadBytes`와 `downloadSizeMaxAgeMs`를 모두 생략하고, HEAD는 응답 전에 새 경계를 측정합니다.

Controller 시작 후 첫 HEAD나 원본 파일·상태 변경 뒤에는 캡처한 파일을 읽고 지표를 다시 구성해야 하며, pending 결과는 짧은 cache가 만료된 뒤 이 작업을 반복합니다. 측정 slot을 기다리거나 측정하는 중 HTTP 요청을 취소하면 server error 응답 없이 요청도 취소됩니다. 저장한 이벤트 로그가 크면 측정 시간이 길어질 수 있습니다. 실행·대기 중 행은 백그라운드 HEAD 측정을 건너뛰지만 snapshot을 직접 다운로드할 때는 크기를 측정하여 정확한 `Content-Length`를 전송합니다.

재시작 후 저장 상태가 `running` 또는 `queued`인 실험은 `interrupted`로 표시하며 ZIP 원본 메타데이터는 변경하지 않습니다. 이는 표시 상태이며 실제 Peer 종료를 증명하지 않습니다. 실시간 상태·카운터를 복원하거나 실행을 자동 재개하지 않지만 ZIP 지표는 보존된 로그에서 재계산합니다. 메타데이터를 읽을 수 없는 항목은 `unreadable`로 표시하므로 로그·저장 파일을 확인하십시오.

**Saved results → Delete**는 확인 후 선택한 run의 시나리오·메타데이터·이벤트를 영구 삭제합니다. 실행·대기 중 실험, 활성 배치 구성원, 다운로드 중 결과는 보호합니다. API는 설정된 bearer token을 사용하는 `DELETE /api/v1/results/{id}`입니다. 기존 Prometheus/Grafana 시계열은 유지하고 삭제 표식으로 지연 telemetry의 결과 재생성을 막습니다. ZIP 원본 복사는 스트리밍하며 지표 재구성에는 고유 이벤트 ID·메시지/수신 쌍에 비례한 메모리가 필요합니다.

실행 창의 **Run** 옆 **Runs**에서 1~100회 순차 반복을 설정합니다. 각 회차는 별도 결과를 남기며 실패 또는 **Stop batch**는 남은 회차를 취소합니다. [반복 실행과 지표 정의](experiment-metrics.kr.md)를 참고하십시오.

저장 결과 목록과 다운로드에도 기존 공개 GET 정책이 적용됩니다. API에서는 다음과 같이 사용할 수 있습니다.

```bash
curl --fail http://localhost:8080/api/v1/results
curl --fail --output run-results.zip \
  http://localhost:8080/api/v1/experiments/RUN_ID/download
```

`RUN_ID`를 저장 목록의 실험 ID로 바꾸십시오. Swarm에서는 control 노드 주소를 사용합니다. `KPL_STACK_NAME=kpl`이면 원본은 해당 노드의 `kpl_controller-data` 볼륨에 있습니다. 이 볼륨을 Controller 내부 `/var/lib/kpl/data`에 마운트하며, 실험별 파일은 그 아래 `runs/<run-id>`에 저장됩니다. 매니저의 `swarm.sh remove`는 이 볼륨을 보존합니다. 원시 이벤트의 자동 보존 기한이나 노드 간 복제는 없으므로 디스크 공간과 백업을 별도로 관리하십시오.

내보내는 내용은 구독 세션 start/checkpoint/stop과 기록된 Agent 종료 확인을 포함한 수집 telemetry입니다. Peer 재시도, Agent 큐 backpressure, 정상 종료 drain은 유실을 줄이지만 한도가 있으며 강제 종료는 Peer를 drain하지 않습니다. 원본 sequence 간격이 있으면 미수신은 unknown입니다. 다운로드로 누락을 복구하거나 아예 보이지 않은 세션을 발견할 수는 없습니다. `/api/v1/events`는 여전히 최근 300개만 반환하고 웹 이벤트 화면은 그중 최신 40개를 표시하지만 ZIP은 저장된 전체 로그를 읽습니다.

## 지표의 의미

| 지표 | 의미 |
|---|---|
| `kpl_events_total` | Controller가 수신한 이벤트 누적 수. `run_id`, `agent_id`, `event_type`, `topic`으로 구분 |
| `kpl_message_bytes_total` | 발행/수신 PubSub data 바이트. libp2p framing 및 TCP/IP 헤더 제외 |
| `kpl_window_stable_pairs`, `kpl_window_reached_pairs` | 기간이 지난 메시지 중 수신 기간 전체의 구독이 증명된 수신 세션 쌍 및 기한 내 성공. `run_id`로 구분 |
| `kpl_window_unknown_pairs`, `kpl_window_missed_pairs`, `kpl_window_late_pairs` | 안정 쌍의 수신 불명·확인된 미도달·마감 후 수신 수 |
| `kpl_window_delivery_ratio` (`bound`: `lower` / `upper`) | 안정 대상 조건부 도달률의 논리적 상·하한. 안정 쌍이 없으면 생략하며 신뢰구간이 아님 |
| `kpl_window_initial_pairs`, `kpl_window_initial_reached_pairs`, `kpl_window_initial_unknown_pairs` | 확인된 발행 시점 대상, 이탈자를 포함한 기한 내 성공, 수신 불명 수 |
| `kpl_window_initial_delivery_ratio` (`bound`: `lower` / `upper`) | 확인된 발행 시점 대상 안의 논리적 도달률 범위. 해당 집단이 비었을 때만 생략 |
| `kpl_window_stable_coverage`, `kpl_window_stable_coverage_upper_bound` | coverage 하한인 안정/확인된 발행 시점 대상과 상한인 (안정 + 지속 여부 불명)/확인된 발행 시점 대상. 확인된 집단이 비었을 때만 생략 |
| `kpl_window_departed_pairs`, `kpl_window_continuity_unknown_pairs` | 마감 전 이탈이 확인된 발행 시점 쌍과, 이탈·마감까지 지속 어느 쪽도 증명하지 못한 쌍 수 |
| `kpl_window_publication_availability_unknown_pairs`, `kpl_window_availability_unknown_pairs` | 발행 시점 생존을 증명하지 못한 후보 쌍과 발행 시점·지속 불명의 합계 |
| `kpl_window_pending_publications`, `kpl_window_finalized_publications` | 마감 전 메시지와 기간이 지난 메시지. 확정 결과도 늦은 telemetry로 정정 가능 |
| `kpl_window_measurement_incomplete`, `kpl_window_legacy_publications` | 관측된 측정 범위 불명 stream 여부(0/1)와 과거 정의 발행 수. 0도 아예 보이지 않은 telemetry는 검출하지 못함 |
| `kpl_window_propagation_latency_seconds` | 안정 대상의 기한 내 첫 원격 envelope 수신 지연. `run_id`, 수신 `agent_id`, `topic`으로 구분. raw·로컬·마감 후·이탈·불명·무효 표본 제외 |
| `kpl_window_duplicate_copies`, `kpl_window_duplicates_per_reached_pair` | 안정 대상의 원격 성공 쌍에서 기간 안에 관측한 추가 PubSub 복사본 및 성공당 평균 |
| `kpl_operation_failures_total` | `onError: continue` 정책에서 기록한 publish/leave 실패 |
| `kpl_telemetry_dropped_events_total` | 보고된 telemetry 유실. P2P 패킷 손실이나 미보고 유실이 0이라는 증거가 아님 |
| `kpl_nodes` | 실험·Agent·그룹·역할·타입·상태별 피어 수 |
| `kpl_agent_*` | Controller에서 관측한 Agent 온라인 상태, 용량, 마지막 heartbeat |
| `kpl_experiment_*` | 실험 상태, 단계, job 상태 |
| `kpl_network_configured_*` | starting/ready 피어의 확정 설정을 그룹별 집계. delay는 평균/최소/최대, jitter/loss는 평균 |
| `kpl_local_*` | 각 Agent의 로컬 피어 상태, 용량, 정리 대기, telemetry 큐 길이 |
| `go_*`, `process_*` | scrape 대상 Controller/Agent 프로세스의 런타임·CPU·메모리. Peer 컨테이너 전체 자원 사용량은 아님 |

`kpl_network_configured_loss_ratio`는 설정한 패킷 손실률이며 실제 관측 손실률이 아닙니다. 설정 delay도 실제 RTT와 구분합니다. graft/prune/remove_peer 이벤트는 PubSub mesh 변화를 분석하는 자료이며 TCP 연결 그래프나 완전한 mesh snapshot을 뜻하지 않습니다.

현재 관계는 Control Room의 [대화형 토폴로지](topology.kr.md)에서 Peer 상태 snapshot을 통해 transport, Kademlia 라우팅 테이블, GossipSub mesh를 독립적으로 확인하십시오. 이 live snapshot은 최근 이벤트 버퍼와 독립적이며 Prometheus나 결과 ZIP에 전체 그래프 이력을 저장하는 것은 아닙니다.

`session-window-v1`은 실제 발행 시각과 `publish.deliveryWindow`(기본 10초, 양수·최대 1시간)를 사용합니다. 주 조건부 도달률은 기간 전체의 구독을 세션 증거로 확인해야 합니다. 마감 전 이탈은 조기 성공했어도 제외하며 발행 시점 대상 도달률에는 유지합니다. 늦은 join과 발행자 로컬 수신은 양쪽 모두 제외합니다. 단절이나 mesh 변화로 구독자를 제거하지 않습니다. 안정 도달률 범위, 발행 시점 대상 범위, coverage, pending, 불명을 함께 보십시오. sequence 누락은 확인된 미도달이 아닌 unknown이며 가용성 증거 부재도 확정 이탈은 아닙니다.

Grafana 세션 패널에는 Run 필터만 적용하고 백분율 평균이 아닌 수신 쌍 합계로 집계합니다. 범위는 관측된 집단에 대한 값이며 보이지 않는 모집단을 증명하지 않습니다. 발행 시점 가용성 경고나 `measurementIncomplete`가 있어도 확인된 발행 시점 도달률과 안정 coverage를 표시하며, 선택한 run 전체에 확인된 발행 시점 쌍이 하나도 없을 때만 N/A입니다. 각 집계에서 확인된 발행 시점 대상 = 안정 + 이탈 + 지속 여부 불명이 성립합니다. 과거 발행 패널은 새 결과에서 제외한 과거 데이터를 표시합니다. 새 패널은 `kpl_window_*`만 사용하고 이전 `kpl_delivery_*`·`kpl_propagation_latency_seconds`는 과거 의미를 유지하므로 섞지 마십시오.

전체 `deliver`에는 로컬 수신도 포함되므로 발행 수로 나누어 도달률을 구하지 않습니다. TCP 재전송은 패킷 손실을 지연으로 바꿀 수 있습니다. [정의·수식·참고 문헌](experiment-metrics.kr.md)을 확인하십시오. 늦은 배치가 기간 gauge와 histogram 버킷을 정정할 수 있어 `rate`/`increase` 대신 직접 조회합니다. Grafana 지연은 실험 전체 누적 분위수이며 선택 시간 범위만의 분위수가 아닙니다.

누적 카운터는 최근 300개 웹 이벤트 버퍼와 독립적입니다. Controller 프로세스가 재시작되면 카운터가 초기화되며 과거 `events.jsonl`을 자동 재생하지 않습니다. Prometheus에 이미 저장된 시계열은 유지되고 `rate`/`increase`는 관측된 카운터 재설정을 처리합니다. 단, scrape 전에 사라진 이벤트나 telemetry 전송 실패를 복구하는 기능은 아닙니다. `increase`는 scrape 표본으로 추정한 구간 증가량이므로 누적 정수 이벤트 수와 항상 정확히 일치하지는 않습니다.

raw 수신도 수신 수·바이트에는 포함되지만 지연 히스토그램에는 포함되지 않습니다. 지연 표본이 없는 구간은 0ms로 해석하지 마십시오. 각 Peer는 시작 시 최대 5초 동안 Controller health 표본을 최대 7개 수집하고, 빠른 실패 사이를 250ms 띄운 뒤 최소 RTT 표본의 중간점을 사용합니다. 실패해도 Ready 진행을 막지 않습니다. 미동기화 상태에서는 5초마다 다시 시도하고 동기화 상태에서는 30초마다 갱신합니다. 성공한 표본은 2분 후 만료되며 마지막 offset은 timestamp 연속성을 위해 유지하지만, 재동기화할 때까지 신뢰 시계 metadata를 중단합니다. 범위로 기한 내 수신을 증명할 수 있는 음수 추정은 도달 성공에 포함하지만 지연 histogram에서는 제외합니다. 모든 호스트에서 chrony/NTP를 계속 사용하십시오. checkpoint는 계측 애플리케이션 세션의 증거이며 물리적 uptime을 뜻하지 않습니다.

## 실제 실행 검증

아래 과거 실행은 세션 기간 측정 이전이며 로컬 지연을 포함합니다. 이전 이벤트 집계 검증이며 새 조건부 도달률이나 지연 분포의 검증은 아닙니다. 새 실행은 같은 `session-window-v1` 정의와 수신 기간으로 비교하십시오.

2026-09-04에 `examples/monitoring.yaml`을 실행하여 아래 결과를 원본 `events.jsonl`과 대조했습니다. 실행 ID는 `run-20260904T024349Z-345a`이며 실험 종료 후 테스트 Peer 컨테이너는 모두 정리되었습니다.

| 확인 항목 | 결과 |
|---|---|
| 발행 / 수신 | 66 / 264건으로 Prometheus와 원본 로그 일치 |
| 누적 이벤트 | 583건 유지, 웹 최근 이벤트 버퍼는 300건 |
| 지연 표본 | envelope 수신 240건만 포함, raw 수신 24건 제외 |
| worker 네트워크 설정 | delay 50ms, jitter 5ms, loss 1% |
| telemetry 큐 유실 | 0건 |

이 수치는 해당 실행의 검증 결과입니다. 중복 수신·mesh 이벤트 수와 측정 지연은 실행 환경에 따라 달라집니다.

Peer 이탈이 처음 보고될 때 시계열이 생성되어 초기 증가량을 놓치는 문제를 줄이기 위해 heartbeat 시 `add_peer`/`remove_peer`의 0 기준값도 생성합니다. 수정 후 재실행(`run-20260904T025651Z-ee92`)에서 발행 66건·수신 264건·지연 표본 240건을 다시 확인했고, Peer 이탈 3건이 구간 증가량에도 반영되었습니다. 아주 짧은 실험에서 첫 scrape 전에 발생한 이벤트는 여전히 증가량 추정에서 누락될 수 있으므로 정확한 건수는 누적 카운터나 원본 로그와 대조하십시오.

## 보존과 운영

Prometheus 시계열은 `prometheus-data`, Grafana 설정은 `grafana-data` named volume에 저장됩니다. 일반적인 컨테이너 재생성 후에도 유지됩니다. Prometheus 보존 설정은 15일/5GB이며 먼저 도달한 정책이 적용됩니다. 5GB는 디스크 사용량의 엄격한 상한이 아니며 WAL·head·압축 작업에는 추가 공간이 필요합니다. [Prometheus 저장소 문서](https://prometheus.io/docs/prometheus/latest/storage/)

Compose bind mount는 `monitoring/grafana/dashboards`의 JSON 수정 사항을 30초 간격으로 반영합니다. Swarm config는 불변이므로 이번 변경은 버전이 바뀐 dashboard config 참조를 사용하여 stack을 재배포해야 적용됩니다. 배포된 원본은 파일로 관리하며 별도 대시보드를 저장하려면 관리자 로그인 후 복사본을 사용하십시오. [Grafana provisioning 문서](https://grafana.com/docs/grafana/latest/administration/provisioning/)

```bash
# 수집 대상 상태 확인
curl http://localhost:9090/api/v1/targets

# Prometheus 설정 검사
docker compose exec prometheus promtool check config /etc/prometheus/prometheus.yml

# 서비스 상태/로그 확인
docker compose ps
docker compose logs --tail=100 prometheus grafana
```

여러 호스트에 Agent를 배치할 때는 `monitoring/prometheus/prometheus.yml`의 Agent targets를 실제 접근 가능한 `/metrics` 주소로 변경하십시오. 로컬 Compose 네트워크 이름만으로 원격 호스트를 발견하지는 않습니다.

현재 이미지 버전은 Prometheus `v3.13.2`와 Grafana `13.2.1`로 고정했습니다. 업데이트 시 공식 [Prometheus 다운로드](https://prometheus.io/download/)와 [Grafana Docker 설치 문서](https://grafana.com/docs/grafana/latest/setup-grafana/installation/docker/)를 참고하고 설정·대시보드를 재검증하십시오.
