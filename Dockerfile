# Build stage - Using Alpine with CGO for SQLite support
FROM golang:1.25-alpine AS builder

# Install build dependencies including GCC for CGO/SQLite
RUN apk add --no-cache git python3 py3-pip gcc musl-dev sqlite-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Note: WASM decoder files are pre-built and included in the repo
# web/static/scanner/wasm/decoder.wasm and wasm_exec.js

# Install OCR parser dependencies in virtualenv
RUN python3 -m venv /opt/ocr-venv && \
    /opt/ocr-venv/bin/pip install --upgrade pip && \
    /opt/ocr-venv/bin/pip install -r tools/ocr_parser/requirements.txt && \
    chmod +x tools/ocr_parser/parser.py

# Build the application with CGO enabled for SQLite
RUN CGO_ENABLED=1 GOOS=linux go build -o server cmd/server/main.go

# Production stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests + python runtime + SQLite
RUN apk --no-cache add ca-certificates tzdata python3 sqlite

# Create app directory
WORKDIR /app

# Create non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Copy binary from builder stage
COPY --from=builder /app/server .

# Copy python virtualenv and parser tool
COPY --from=builder /opt/ocr-venv /opt/ocr-venv
COPY --from=builder /app/tools/ocr_parser tools/ocr_parser

# Copy web assets
COPY --chown=appuser:appgroup web/ web/
COPY --chown=appuser:appgroup migrations/ migrations/
COPY --chown=appuser:appgroup keys/ keys/

# Create directories for uploads and logs
RUN mkdir -p uploads logs archives && \
    chown -R appuser:appgroup uploads logs archives

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["./server"]
