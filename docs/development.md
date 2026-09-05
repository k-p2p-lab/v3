# Development Guide

English | [Korean](development.kr.md)

The canonical Go module path is `github.com/k-p2p-lab/v3`, and the project requires Go 1.24 or later. Build and test from the repository root:

```sh
go build -o bin/kpl ./cmd/kpl
go test ./...
```

Validate a scenario without running it:

```sh
./bin/kpl validate --scenario examples/smoke.yaml
```

For quick development without Docker, run the Controller and one Agent in separate terminals:

```sh
./bin/kpl controller --listen :8080
./bin/kpl agent --runtime process --id local-a \
  --advertise-url http://127.0.0.1:8090 \
  --controller-url http://127.0.0.1:8080
```

The process runtime starts each Peer as a child process. It cannot isolate network namespaces and rejects scenarios that configure Peer network conditions. Use the default Docker runtime and the [Linux preflight](linux-deployment.md#prepare-and-start-the-server) when validating container isolation or `tc` behavior.

Run the full suite in the Linux test image when Go is not installed on the target host:

```sh
make test-linux
```

The runtime image uses `CGO_ENABLED=0`. Shell scripts, Dockerfiles, and Makefiles retain LF endings through `.gitattributes`.
