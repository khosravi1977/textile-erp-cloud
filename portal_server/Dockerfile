FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
# Apply deterministic security/RBAC hardening inside the build context. The
# transformer fails the build if an expected source anchor drifts, so a future
# refactor cannot silently restore password exposure or role escalation.
RUN go run ./tools/hardener -file main.go && gofmt -w main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/textile-erp-portal .

FROM alpine:3.22

WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata wget

COPY --from=builder /out/textile-erp-portal /app/textile-erp-portal
COPY downloads/HesabYar.apk /app/downloads/HesabYar.apk

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app/textile-erp-portal"]
