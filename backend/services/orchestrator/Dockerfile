# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates

ENV CGO_ENABLED=0
ENV GOTOOLCHAIN=local

COPY go.work* ./
COPY contracts ./contracts
COPY libs ./libs
COPY services ./services

RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  cd services/orchestrator && \
  go build -trimpath -ldflags="-s -w" -o /out/orchestrator ./cmd/orchestrator

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/orchestrator /orchestrator

USER nonroot:nonroot

EXPOSE 8084

ENTRYPOINT ["/orchestrator"]
