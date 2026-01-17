# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Instalar certificados e git
RUN apk add --no-cache ca-certificates git

# Copiar go.mod e go.sum primeiro (cache de dependencias)
COPY go.mod go.sum ./
RUN go mod download

# Copiar codigo fonte
COPY . .

# Build com flags de otimizacao (usa arquitetura nativa do builder)
RUN CGO_ENABLED=0 go build \
    -ldflags="-w -s" \
    -o /wega-api \
    ./cmd/server

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Certificados SSL e timezone
RUN apk add --no-cache ca-certificates tzdata

# Copiar binario
COPY --from=builder /wega-api .

# Usuario nao-root
RUN adduser -D -u 1000 appuser
USER appuser

# Porta da API
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Executar
CMD ["./wega-api"]
