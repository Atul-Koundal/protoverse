# ---- Build stage ----
FROM golang:1.26-alpine AS builder
WORKDIR /app

# Cache dependency downloads separately from source changes
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/tickengine ./cmd/tickengine
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/web ./cmd/web
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/pvctl ./cmd/pvctl

# ---- api runtime image ----
FROM alpine:3.20 AS api
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/api /usr/local/bin/api
EXPOSE 50051
ENTRYPOINT ["/usr/local/bin/api"]

# ---- tickengine runtime image ----
FROM alpine:3.20 AS tickengine
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/tickengine /usr/local/bin/tickengine
ENTRYPOINT ["/usr/local/bin/tickengine"]

# ---- web runtime image (browser client + JSON/SSE gateway) ----
FROM alpine:3.20 AS web
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/web /usr/local/bin/web
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/web"]

# ---- pvctl command-line client (docker run --network host ... pvctl status) ----
FROM alpine:3.20 AS pvctl
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/pvctl /usr/local/bin/pvctl
ENTRYPOINT ["/usr/local/bin/pvctl"]
