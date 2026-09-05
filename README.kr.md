# K-P2PLab v3

[English](README.md) | 한국어

K-P2PLab v3는 하나 이상의 Linux 호스트에서 재현 가능한 libp2p Kademlia 및 PubSub 실험을 실행합니다. Controller가 시나리오를 스케줄링하고 호스트마다 Agent 하나가 동작하며, 각 Peer는 독립된 Docker 컨테이너와 네트워크 네임스페이스에서 실행됩니다. 웹 Control Room과 기본 제공 Prometheus/Grafana 스택에서 토폴로지, churn, 전파와 저장 결과를 확인할 수 있습니다.

코드, UI와 문서의 기본 언어는 영어이며 한국어 문서는 대응하는 `.kr.md` 파일로 유지합니다.

## 핵심 기능

- join, leave, 준비 장벽, publish, phase 반복, 백그라운드 잡과 seeded distribution을 지원하는 버전 2 YAML 시나리오
- Peer별 delay, jitter, loss, duplication, corruption, reordering과 bandwidth 설정을 적용하는 격리 컨테이너
- 단일 호스트 Docker Compose 및 용량 기반 Peer 배치를 지원하는 다중 서버 Docker Swarm 배포
- Agent 영역과 topic 필터를 제공하는 실시간 Kademlia, GossipSub GRAFT 및 transport 토폴로지
- churn을 고려한 도달률, 지연, 중복, coverage와 관측 품질 지표
- 재사용 가능한 시나리오 라이브러리, 반복 실행, 결과 보존, ZIP 내보내기와 삭제

## 사전 요구사항

- rootful Docker Engine과 Docker Compose v2가 설치된 Linux. 기본 배포 구성은 userns-remap을 지원하지 않습니다.
- 네트워크 조건을 위한 `NET_ADMIN`과 커널 `sch_prio`, `sch_netem`, `cls_u32` 및 선택적인 `sch_tbf` 모듈
- Docker 밖에서 개발할 때만 Go 1.25 이상
- Swarm에서는 활성 manager와 선택한 모든 노드가 접근하고 신뢰하는 이미지 registry

권한, 원격 접속, 저장소와 안전한 종료 방법은 [Linux 배포 가이드](docs/linux-deployment.kr.md)를 참고하십시오.

## 로컬 빠른 시작

```sh
test -f .env || cp .env.example .env
# .env에서 KPL_API_TOKEN과 GRAFANA_ADMIN_PASSWORD를 설정합니다.
docker compose build controller
sh scripts/check-linux.sh
docker compose up -d --no-build
```

[Control Room](http://localhost:8080), [Grafana](http://localhost:3000/d/kpl-experiments), [Prometheus](http://localhost:9090)를 여십시오. Control Room에서 기본 시나리오를 실행할 수 있습니다. 종료할 때는 `make stop`을 사용하십시오.

## Swarm 빠른 시작

활성 manager의 저장소 디렉터리에서 아래 명령을 실행합니다. 이미지 주소는 모든 노드가 접근할 수 있는 registry로 바꾸십시오.

```sh
sh scripts/swarm.sh init KPL_IMAGE=registry.example.com/kpl-v3:v3 KPL_AGENT_CAPACITY=20 KPL_MIN_AGENTS=2
sh scripts/swarm.sh publish
sh scripts/swarm.sh deploy --workers
sh scripts/swarm.sh status
sh scripts/swarm.sh access
sh scripts/swarm.sh credentials
sh scripts/swarm.sh scenario
```

manager에도 Agent를 실행해야 한다면 `--workers` 대신 `--all`을 사용합니다. `access`가 출력한 Controller 주소를 열고 `scenario` 출력 내용을 붙여 넣은 다음 `credentials`가 출력한 API token을 사용하십시오. helper는 배포 시점의 이미지 digest를 확인해 고정하므로 tag를 갱신할 때 SHA를 직접 수정할 필요가 없습니다. 운영 클러스터를 관리하거나 철거하기 전에 [전체 Swarm 절차](docs/swarm.kr.md)를 확인하십시오.

## 문서

| 문서 | 내용 |
|---|---|
| [Linux 배포](docs/linux-deployment.kr.md) | 단일 호스트 준비, 권한, 저장소, 원격 접속과 종료 |
| [Swarm 배포](docs/swarm.kr.md) | registry 설정, 노드 선택, 배포, 업데이트, 확장과 철거 |
| [시나리오 설정](docs/scenario-reference.kr.md) | YAML action, profile, 프로토콜 설정, distribution과 네트워크 조건 |
| [시나리오 라이브러리](docs/scenario-library.kr.md) | 재사용할 시나리오의 저장, 이름 지정, 불러오기, 갱신과 삭제 |
| [REST API](docs/api.kr.md) | Controller endpoint, 인증, 결과와 내부 정리 API |
| [개발](docs/development.kr.md) | Go 빌드, 검증, 테스트와 로컬 process 런타임 |
| [실험 지표](docs/experiment-metrics.kr.md) | churn 상황의 도달률 분모, 지연, 중복과 한계 |
| [모니터링과 결과](docs/monitoring.kr.md) | Prometheus, Grafana, ZIP 내용, 보존 데이터와 삭제 |
| [토폴로지](docs/topology.kr.md) | Agent 영역, 그래프 레이어, topic 필터와 조작 방법 |
| [Swarm churn 및 publish](docs/swarm-churn-publish.kr.md) | 다중 서버 연속 churn 실험 절차 |
| [v2 재현](docs/v2-reproduction.kr.md) | K-P2PLab v2 호환 매핑과 의도적인 차이 |
