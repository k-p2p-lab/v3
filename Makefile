.PHONY: build test validate run

build:
	go build -o bin/kpl ./cmd/kpl

test:
	go test ./...

validate:
	go run ./cmd/kpl validate --scenario examples/smoke.yaml

run:
	docker compose up --build
