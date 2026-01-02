FROM golang:1.25.5 AS builder

WORKDIR /app

# Download and install dependencies
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

# Build the binary
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -v -ldflags="-w -s" -o givetypst .

FROM alpine:latest@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412 AS runner

RUN apk --no-cache add ca-certificates

# Install Typst
RUN apk add --no-cache curl tar xz && \
    curl -LO https://github.com/typst/typst/releases/download/v0.14.2/typst-x86_64-unknown-linux-musl.tar.xz && \
    tar -xf typst-x86_64-unknown-linux-musl.tar.xz && \
    mv typst-x86_64-unknown-linux-musl/typst /usr/local/bin/ && \
    rm -rf typst-* && \
    apk del curl tar xz

WORKDIR /app
COPY --from=builder /app/givetypst /usr/local/bin/givetypst
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["givetypst"]
