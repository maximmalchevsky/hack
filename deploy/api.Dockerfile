# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder
WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /out/api \
    ./cmd/api

FROM alpine:3.20 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates wget tzdata && \
    update-ca-certificates

COPY --from=builder /out/api /app/api

EXPOSE 8080

USER nobody
ENTRYPOINT ["/app/api"]
