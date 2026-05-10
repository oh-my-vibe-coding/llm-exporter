# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /src

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w \
        -X 'github.com/oh-my-vibe-coding/llm-exporter/internal/version.Version=$(VERSION)' \
        -X 'github.com/oh-my-vibe-coding/llm-exporter/internal/version.GitCommit=$(GIT_COMMIT)' \
        -X 'github.com/oh-my-vibe-coding/llm-exporter/internal/version.BuildTime=$(BUILD_TIME)'" \
    -o /llm-exporter ./cmd/llm-exporter

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates && \
    addgroup -S appgroup && adduser -S appuser -G appgroup
COPY --from=builder /llm-exporter /usr/local/bin/llm-exporter

EXPOSE 9101
USER appuser
ENTRYPOINT ["llm-exporter"]
CMD ["--config", "/etc/llm-exporter/config.yaml"]
