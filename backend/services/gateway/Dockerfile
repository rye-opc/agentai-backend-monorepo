# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates

ENV CGO_ENABLED=0
ENV GOTOOLCHAIN=local

COPY go.work go.work.sum ./
COPY contracts ./contracts
COPY libs ./libs
COPY services ./services

RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  cd services/gateway && \
  go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/gateway /gateway

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/gateway"]
