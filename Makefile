.PHONY: build test test-linux check-linux check-swarm swarm-init swarm-deploy swarm-status swarm-add-node swarm-remove-node swarm-remove validate run stop

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

swarm-init:
	sh scripts/swarm.sh init

swarm-deploy:
	sh scripts/swarm.sh deploy $(NODES)

swarm-status:
	sh scripts/swarm.sh status

swarm-add-node:
	sh scripts/swarm.sh add-node $(NODES)

swarm-remove-node:
	sh scripts/swarm.sh remove-node $(NODES)

swarm-remove:
	sh scripts/swarm.sh remove

validate:
	go run ./cmd/kpl validate --scenario examples/smoke.yaml

run:
	docker compose up --build

# Keep Agents available until the Controller finishes its experiment cleanup.
stop:
	docker compose stop controller
	docker compose down
