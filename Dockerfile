FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/kpl ./cmd/kpl

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S kpl \
    && adduser -S -G kpl kpl
COPY --from=builder /out/kpl /usr/local/bin/kpl
WORKDIR /var/lib/kpl
RUN chown -R kpl:kpl /var/lib/kpl
USER kpl
ENTRYPOINT ["kpl"]
