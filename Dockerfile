FROM golang:1.25.3-alpine AS builder

WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN mkdir -p /out && \
    CGO_ENABLED=0 GOOS=linux go build -p 2 -trimpath -o /out/ ./cmd/...

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app -u 10001 app && \
    mkdir -p /app/config && chown app:app /app

WORKDIR /app
ARG SERVICE_NAME
COPY --from=builder --chown=app:app /out/${SERVICE_NAME} /app/service
COPY --chown=app:app config/config.docker.yaml /app/config/config.yaml
ENV CONFIG_PATH=/app/config/config.yaml TZ=Asia/Shanghai
USER app

CMD ["/app/service"]
