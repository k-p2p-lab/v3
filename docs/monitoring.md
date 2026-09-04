# Prometheus와 Grafana로 실험 분석하기

`docker compose up -d --build`로 Controller, Agent 두 개, Prometheus, Grafana를 함께 실행합니다. 데이터 소스와 **KP2PLab 실험 분석** 대시보드는 자동 등록됩니다.

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

1. Control Room에서 [`examples/monitoring.yaml`](../examples/monitoring.yaml)을 실행합니다. envelope 발행과 raw 발행을 함께 확인하는 작은 실험입니다.
2. Grafana에서 실험(run_id), Agent, 토픽을 선택합니다. 여러 run을 고르면 선택한 실험의 트래픽·지연을 합산하며, 네트워크 설정 시계열은 범례에서 run별로 구분합니다.
3. 실험이 끝난 뒤에도 시간 범위를 해당 실행 구간으로 지정하면 시계열을 볼 수 있습니다. 기본 새로고침은 5초입니다.

Prometheus는 Controller와 두 Agent의 `/metrics`를 5초마다 수집합니다. Peer를 직접 scrape하거나 각 컨테이너에 exporter를 추가하지 않습니다. Controller는 기존 telemetry를 누적 집계하므로 `scope: all`에서 telemetry가 손실되면 지표도 영향을 받습니다. 모니터링 서비스는 별도 Docker network에 있으며 Peer에는 추가 네트워크·권한을 부여하지 않습니다.

## 지표의 의미

| 지표 | 의미 |
|---|---|
| `kpl_events_total` | Controller가 수신한 이벤트 누적 수. `run_id`, `agent_id`, `event_type`, `topic`으로 구분 |
| `kpl_message_bytes_total` | 발행/수신 PubSub data 바이트. libp2p framing 및 TCP/IP 헤더 제외 |
| `kpl_propagation_latency_seconds` | 유효한 envelope 수신 지연의 히스토그램. raw·음수·미측정 값 제외 |
| `kpl_operation_failures_total` | `onError: continue` 정책에서 기록한 publish/leave 실패 |
| `kpl_telemetry_dropped_events_total` | Peer가 보고한 telemetry 큐 포화로 인한 유실 수 |
| `kpl_nodes` | 실험·Agent·그룹·역할·타입·상태별 피어 수 |
| `kpl_agent_*` | Controller에서 관측한 Agent 온라인 상태, 용량, 마지막 heartbeat |
| `kpl_experiment_*` | 실험 상태, 단계, job 상태 |
| `kpl_network_configured_*` | starting/ready 피어의 확정 설정을 그룹별 집계. delay는 평균/최소/최대, jitter/loss는 평균 |
| `kpl_local_*` | 각 Agent의 로컬 피어 상태, 용량, 정리 대기, telemetry 큐 길이 |
| `go_*`, `process_*` | scrape 대상 Controller/Agent 프로세스의 런타임·CPU·메모리. Peer 컨테이너 전체 자원 사용량은 아님 |

`kpl_network_configured_loss_ratio`는 설정한 패킷 손실률이며 실제 관측 손실률이 아닙니다. 설정 delay도 실제 RTT와 구분합니다. graft/prune/remove_peer 이벤트는 PubSub mesh 변화를 분석하는 자료이며 TCP 연결 그래프나 완전한 mesh snapshot을 뜻하지 않습니다.

수신(`deliver`)에는 발행 피어 자신의 로컬 수신도 포함됩니다. 따라서 수신 수/발행 수 비율을 패킷 손실률로 해석할 수 없으며, 지연 히스토그램에도 로컬 전달 표본이 포함됩니다. TCP 재전송으로 패킷 손실이 메시지 유실 대신 지연 증가로 나타날 수도 있습니다.

누적 카운터는 최근 300개 웹 이벤트 버퍼와 독립적입니다. Controller 프로세스가 재시작되면 카운터가 초기화되며 과거 `events.jsonl`을 자동 재생하지 않습니다. Prometheus에 이미 저장된 시계열은 유지되고 `rate`/`increase`는 관측된 카운터 재설정을 처리합니다. 단, scrape 전에 사라진 이벤트나 telemetry 전송 실패를 복구하는 기능은 아닙니다. `increase`는 scrape 표본으로 추정한 구간 증가량이므로 누적 정수 이벤트 수와 항상 정확히 일치하지는 않습니다.

raw 수신도 수신 수·바이트에는 포함되지만 지연 히스토그램에는 포함되지 않습니다. 지연 표본이 없는 구간은 0ms로 해석하지 마십시오. Peer 시계가 다른 호스트에 걸쳐 있으면 시계 동기화가 전파 지연 측정에 영향을 줍니다.

## 실제 실행 검증

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

Grafana 대시보드는 `monitoring/grafana/dashboards`의 JSON을 수정하면 30초 간격으로 반영됩니다. 배포된 원본은 파일로 관리하며 별도 대시보드를 저장하려면 관리자 로그인 후 복사본을 사용하십시오. [Grafana provisioning 문서](https://grafana.com/docs/grafana/latest/administration/provisioning/)

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
