.PHONY: build test test-linux check-linux check-swarm validate run stop

build:
	go build -o bin/kpl ./cmd/kpl

test:
	go test ./...

test-linux:
	docker build --target test -t kpl-v3:test .

check-linux:
	sh scripts/check-linux.sh

check-swarm:
	sh scripts/check-swarm.sh

validate:
	go run ./cmd/kpl validate --scenario examples/smoke.yaml

run:
	docker compose up --build

# Keep Agents available until the Controller finishes its experiment cleanup.
stop:
	docker compose stop controller
	docker compose down
