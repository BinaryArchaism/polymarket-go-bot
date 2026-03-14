# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set build environment
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /build

# Copy dependency files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN go build -ldflags="-s -w" -o /app/bot ./cmd/bot

# Runtime stage
FROM alpine:3.22

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user for security
RUN addgroup -g 1000 -S polybot && \
    adduser -u 1000 -S polybot -G polybot

# Create directories for data and config
RUN mkdir -p /app/data && \
    chown -R polybot:polybot /app

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/bot /app/bot

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Set ownership
RUN chown polybot:polybot /app/bot

# Switch to non-root user
USER polybot

# Health check via process existence
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD pgrep bot || exit 1

ENTRYPOINT ["/app/bot"]
