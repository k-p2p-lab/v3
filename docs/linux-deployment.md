# Linux 운영 가이드

최종 실행 환경은 Linux Docker Engine입니다. Windows는 개발 호스트로 사용할 수 있지만 Peer 실행·네트워크 제어·종료 검증은 Linux 컨테이너에서 수행합니다. 제공하는 Compose 구성은 **한 대의 Linux 서버, rootful Docker, userns-remap 미사용, IPv4 user-defined bridge**를 기준으로 합니다.

## 서버 준비와 실행

Docker Engine과 Compose 플러그인을 설치하고, 같은 사용자와 같은 Docker context로 아래 명령을 실행하십시오. Docker 28 이상을 권장합니다. 이전 버전에는 localhost에 게시한 포트가 같은 L2 네트워크에서 접근될 수 있는 제한이 있습니다. [Docker 포트 게시 문서](https://docs.docker.com/engine/network/port-publishing/)

```sh
# 저장소 디렉터리에서 실행합니다. 기존 .env가 있으면 보존합니다.
test -f .env || cp .env.example .env
chmod 600 .env
# .env에서 KPL_API_TOKEN과 GRAFANA_ADMIN_PASSWORD를 설정합니다.

docker compose build controller
sh scripts/check-linux.sh
docker compose config --quiet
docker compose up -d --no-build
docker compose ps
```

사전 점검은 실행 이미지·Docker 접근·Compose 및 실제 커널의 prio/netem/u32/TBF 설치를 확인합니다. `timeout`과 `setsid` 유틸리티가 필요합니다. 별도 컨테이너를 생성하고 종료 시 제거하며, 호스트 인터페이스나 진행 중인 실험에는 tc 규칙을 적용하지 않습니다. 생성 중 취소 요청은 생성 결과를 확인한 뒤 처리합니다. Docker 응답 지연 등으로 제한 시간 내 정리를 확인하지 못하면 수동 확인할 정확한 컨테이너 이름을 출력합니다. 이미지 이름을 바꾼 구성은 `KPL_DOCKER_IMAGE`로 검사 대상을 지정할 수 있습니다.

`NET_ADMIN`과 커널의 `sch_prio`, `sch_netem`, `cls_u32`, TBF 사용 시 `sch_tbf` 지원이 필요합니다. 모듈은 커널에 내장되어 있을 수도 있으므로 `lsmod` 출력만으로 지원 여부를 판단하지 않습니다. 설치 실패 시 대상 서버의 커널 설정과 모듈 제공 상태를 확인하십시오. Peer가 호스트에서 모듈을 로드하도록 권한을 부여하지 않습니다.

## 원격 화면 접속

Controller 8080, Grafana 3000, Prometheus 9090은 기본적으로 서버의 `127.0.0.1`에 게시됩니다. 개발 PC에서 SSH 터널을 연결하면 기존 localhost 주소를 그대로 사용할 수 있습니다.

```sh
ssh -N -L 8080:127.0.0.1:8080 -L 3000:127.0.0.1:3000 -L 9090:127.0.0.1:9090 user@linux-server
```

Controller를 관리망 주소에 직접 게시해야 한다면 `.env`의 `KPL_BIND_ADDRESS`와 `KPL_HTTP_PORT`를 지정하십시오. `KPL_API_TOKEN`은 변경 API를 보호하며 GET 조회를 인증하는 설정은 아닙니다. Grafana 관리자 비밀번호와 읽기 전용 익명 접속 설정은 [모니터링 가이드](monitoring.md)를 참고하십시오.

## 종료와 서버 재시작

Controller가 실행 중인 실험을 취소하고 Agent에 Peer 정리를 요청할 수 있도록 **Controller를 먼저 정지한 뒤** 나머지 서비스를 내립니다.

```sh
docker compose stop controller
docker compose down
# make가 설치되어 있으면 make stop도 같은 순서로 실행합니다.
```

Controller는 SIGTERM 시 신규 실험 접수를 중단하고 HTTP 연결·실험 작업·Peer 정리·최종 실행 상태 저장을 기다립니다. 기본 `jobShutdownTimeout: 3m`이 job 종료와 Peer 정리에 각각 적용되므로 Compose의 Controller 종료 유예를 `7m`, Agent를 `3m`로 설정했습니다. 더 긴 `jobShutdownTimeout`을 사용하는 시나리오는 Controller 종료 유예도 함께 늘리십시오. 외부 systemd 단위로 Compose를 관리한다면 해당 단위의 종료 제한이 이 유예를 먼저 끊지 않아야 합니다.

`docker compose down`만 실행하면 의존성 역순으로 Agent가 먼저 정지할 수 있습니다. Agent도 자체적으로 Peer를 정리하지만 Controller의 최종 상태 기록까지 확보하려면 위 순서를 사용하십시오. 강제 종료·전원 손실 후에는 Agent 시작 시 같은 Agent ID/네트워크의 잔존 관리 컨테이너를 회수합니다. 이전 실험을 자동 재개하지는 않습니다.

Controller는 데이터 디렉터리에 쓸 수 없는 경우 시작 단계에서 오류를 반환합니다. 실행 메타데이터는 임시 파일에 기록하고 닫은 뒤 같은 파일시스템에서 rename으로 교체하므로 Linux에서 읽는 도중 일부 JSON만 보이지 않도록 합니다. 매 phase의 fsync가 실험 간격과 telemetry 처리를 지연시키지 않도록 강제 동기화는 하지 않습니다. 이벤트 로그는 append 형식이며, 두 파일 모두 전원 손실 시 모든 기록의 디스크 보존을 보장하지 않습니다.

## Docker 권한과 저장소

Agent만 Docker socket을 사용하며 컨테이너 내부에서 `0:0`으로 실행됩니다. Peer에는 socket·호스트 디렉터리·호스트 포트를 전달하지 않습니다. Peer도 설정 파일 접근을 위해 `0:0`으로 실행하지만 기본 capability를 모두 제거하고 `no-new-privileges`를 적용하며, 네트워크 조건이 있을 때만 `NET_ADMIN`을 추가합니다. Docker socket을 사용할 수 있는 Agent는 서버의 Docker 컨테이너를 관리할 수 있는 구성 요소입니다.

기본 경로는 `/var/run/docker.sock`입니다. Rootless Docker는 socket 위치와 네트워크 namespace가 다르며 overlay도 지원하지 않으므로 제공된 Compose의 지원 기준에 포함하지 않습니다. userns-remap 역시 Agent의 socket 접근 권한을 별도 구성해야 합니다. 이를 해결하려고 socket을 전체 사용자에게 쓰기 허용하거나 Peer를 privileged로 실행하지 마십시오. [Docker Rootless 제한](https://docs.docker.com/engine/security/rootless/troubleshoot/)

실험 기록·Prometheus·Grafana 데이터는 named volume에 저장합니다. `docker compose down`은 보존하지만 `down -v`는 삭제하므로 일반 종료 명령에 사용하지 않습니다. bind mount로 데이터 저장소를 바꾸는 경우 컨테이너 실행 UID/GID의 쓰기 권한을 확인하십시오. Docker daemon이 원격이면 bind mount의 소스 경로도 daemon 호스트 기준이므로 이 저장소와 명령은 실제 배포 서버에 두는 것을 권장합니다. [Docker bind mount 문서](https://docs.docker.com/engine/storage/bind-mounts/)

SELinux enforcing 서버에서는 `monitoring/`의 읽기 권한과 컨테이너용 파일 label을 확인해야 합니다. 필요한 경우 해당 프로젝트의 모니터링 bind mount에만 `:ro,z`를 적용하십시오. `/var/run/docker.sock`이나 시스템 디렉터리를 재라벨링하지 말고, Agent의 socket 접근은 서버의 컨테이너 정책에 맞게 허용해야 합니다. 현재 검증 환경에는 SELinux가 없으므로 이 정책은 대상 서버에서 확인해야 합니다. AppArmor/SELinux를 전체 비활성화하는 설정은 포함하지 않습니다.

## 네트워크 실험의 범위

Peer는 IPv4 컨테이너 주소와 TCP 20000으로 통신하며, 제어 API는 TCP 18000입니다. `scope: p2p`는 TCP 20000 송신만, `scope: all`은 제어·telemetry를 포함한 송신을 제한합니다. IPv6 전용 실험, host network, 컨테이너 IP에 직접 접근할 수 없는 원격 Agent 구성은 지원 기준 밖입니다.

커널이 큰 jitter를 조용히 줄여 적용하는 문제를 피하도록 `jitter`의 최대값을 `2.147483647s`로 검증합니다. Linux 5.15/6.8의 netem 지연 속성 호환 경로를 검토했으며, 최신 커널에서 추가된 netem seed 옵션은 전송하지 않습니다. 실제 지원 여부는 서버에서 사전 점검으로 확인하십시오. [Linux 6.8 netem 구현](https://github.com/torvalds/linux/blob/v6.8/net/sched/sch_netem.c)

다중 물리 호스트에서는 attachable overlay 등 실제 호스트 간 연결을 별도로 준비하고 모든 Agent·Peer가 같은 네트워크에서 서로 접근하도록 해야 합니다. 기본 Compose bridge는 한 Docker 호스트에만 적용됩니다. 모든 호스트에 같은 실행 이미지를 준비하고 Agent ID를 중복하지 마십시오. 지연 측정을 비교하려면 chrony/NTP 등으로 호스트 시계를 동기화하십시오. [Docker overlay 요건](https://docs.docker.com/engine/network/drivers/overlay/)

## Linux 검증 경로

```sh
# 호스트의 Go 설치 없이 Linux에서 전체 Go 테스트를 실행합니다.
docker build --target test -t kpl-v3:test .
# 또는 make test-linux
```

실행 이미지는 `CGO_ENABLED=0`으로 빌드하며 최종 Alpine 이미지에 Go 빌드 도구를 포함하지 않습니다. `.gitattributes`는 shell 스크립트·Dockerfile·Makefile의 LF 줄바꿈을 유지합니다. 스크립트는 `sh scripts/check-linux.sh`로 실행하므로 Windows checkout의 실행 비트에 의존하지 않습니다.

현재 검증에 사용한 커널은 Docker Desktop의 Linux `6.18.33.2-microsoft-standard-WSL2`, 아키텍처는 amd64입니다. 이는 실제 Linux syscall·namespace·tc 검증이지만 대상 서버의 배포판·커널·SELinux·다중 호스트 overlay·대규모 성능 검증을 대체하지 않습니다. 배포할 서버에서 사전 점검과 `examples/monitoring.yaml`을 다시 실행하여 설정과 수집 결과를 확인하십시오.

### 2026-09-04 검증 결과

| 항목 | 결과 |
|---|---|
| Linux 전체 Go 테스트 | 모든 패키지 통과. 동시 JSON 읽기와 Controller 종료 대기 테스트 포함 |
| ARM64 | `GOOS=linux GOARCH=arm64 CGO_ENABLED=0` 교차 빌드 통과, ELF AArch64 확인. ARM64 기기 실행은 미검증 |
| 커널 사전 점검 | 공식 Linux Docker CLI/Compose 컨테이너에서 실행, prio/netem/u32/TBF 설치·정리 통과 |
| 실제 TCP 조건 | 별도 컨테이너 두 개에서 P2P 100% 손실·제어 포트 보존, 100ms 지연, TBF 1Mbit 효과, `scope: all` 제어 손실 확인 |
| 권한 | NET_ADMIN이 없으면 tc 실패. Controller 데이터 디렉터리가 읽기 전용이면 시작 시 오류·exit 1 확인 |
| SIGTERM | 실행 중인 `run-20260904T031927Z-0b3e`의 준비된 Peer 3개를 정리하고 약 16.72초 뒤 exit 0, 잔존 컨테이너 0개 |
| 최종 상태 | 정지한 Controller의 `experiment.json`에서 `canceled` 및 `finishedAt` 확인 후 서비스 재시작 |

단위 테스트의 600개 컨테이너 ID 목록은 대량 조회/정리 로직을 검증하기 위한 모의 응답이며 실제 600개 Peer 부하 측정 결과는 아닙니다.
