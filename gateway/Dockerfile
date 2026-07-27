FROM ubuntu:24.04 AS builder

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends golang-go ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod ./
COPY go.sum ./
COPY cmd ./cmd
COPY internal ./internal

RUN go build -trimpath -ldflags="-s -w" -o /out/inference-gateway ./cmd/inference-gateway

FROM ubuntu:24.04

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tzdata \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /data

COPY --from=builder /out/inference-gateway /usr/local/bin/inference-gateway
COPY config/inference_time_benchmarks.json /app/config/inference_time_benchmarks.json

ENV SERVICE_PORT=9010

EXPOSE 9010

VOLUME ["/data"]

ENTRYPOINT ["inference-gateway"]
