# ---- Build stage ----
FROM golang:1.26-alpine AS builder
WORKDIR /app

# Cache dependency downloads separately from source changes
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/tickengine ./cmd/tickengine

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
