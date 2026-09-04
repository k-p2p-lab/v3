FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/kpl ./cmd/kpl

FROM builder AS test
RUN sh scripts/test-swarm-agent.sh && sh scripts/test-check-swarm.sh && sh scripts/test-swarm.sh
RUN CGO_ENABLED=0 go test -buildvcs=false ./... -timeout 120s

FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates tzdata docker-cli iproute2 iproute2-tc \
    && addgroup -S kpl \
    && adduser -S -G kpl kpl
COPY --from=builder /out/kpl /usr/local/bin/kpl
COPY scripts/swarm-agent.sh /usr/local/bin/kpl-swarm-agent.sh
WORKDIR /var/lib/kpl
RUN mkdir -p /var/lib/kpl/data /var/lib/kpl/agent \
    && chown -R kpl:kpl /var/lib/kpl
USER kpl
ENTRYPOINT ["kpl"]
