# Stage 1: Build
FROM golang:1.21-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
ENV GOPROXY=https://proxy.golang.org|direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /textile-erp ./cmd/api

# Stage 2: Run
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Asia/Tehran
WORKDIR /app
COPY --from=builder /textile-erp .
COPY --from=builder /app/internal/infrastructure/persistence/postgres/migrations ./internal/infrastructure/persistence/postgres/migrations

EXPOSE 8081
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://localhost:8081/health || exit 1

CMD ["./textile-erp"]
