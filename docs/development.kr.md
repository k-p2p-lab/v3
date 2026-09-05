# 개발 가이드

[English](development.md) | 한국어

정식 Go 모듈 경로는 `github.com/k-p2p-lab/v3`이며 Go 1.25 이상이 필요합니다. 저장소 루트에서 빌드와 테스트를 실행하십시오.

```sh
go build -o bin/kpl ./cmd/kpl
go test ./...
```

실행하지 않고 시나리오를 검증하는 명령은 다음과 같습니다.

```sh
./bin/kpl validate --scenario examples/smoke.yaml
```

Docker 없이 빠르게 개발하려면 별도 터미널에서 Controller와 Agent 하나를 실행합니다.

```sh
./bin/kpl controller --listen :8080
./bin/kpl agent --runtime process --id local-a \
  --advertise-url http://127.0.0.1:8090 \
  --controller-url http://127.0.0.1:8080
```

process 런타임은 각 Peer를 자식 프로세스로 실행합니다. 네트워크 네임스페이스를 격리할 수 없으므로 Peer 네트워크 조건이 있는 시나리오를 거부합니다. 컨테이너 격리 또는 `tc` 동작을 검증할 때는 기본 Docker 런타임과 [Linux 사전 점검](linux-deployment.kr.md#서버-준비와-실행)을 사용하십시오.

대상 호스트에 Go가 없으면 Linux 테스트 이미지에서 전체 suite를 실행할 수 있습니다.

```sh
make test-linux
```

런타임 이미지는 `CGO_ENABLED=0`으로 빌드합니다. 셸 스크립트, Dockerfile과 Makefile은 `.gitattributes`를 통해 LF line ending을 유지합니다.
