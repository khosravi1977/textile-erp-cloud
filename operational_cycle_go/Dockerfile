FROM node:20-alpine AS web-builder

WORKDIR /web
COPY web/package*.json ./
RUN npm install
COPY web ./
RUN npm run build

FROM golang:1.24-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git
ENV GOPROXY=https://proxy.golang.org|direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/operational-cycle ./cmd/server

FROM alpine:3.22

WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata wget

ENV PORT=8091
ENV OPERATIONAL_DB_DRIVER=postgres

COPY --from=builder /out/operational-cycle /app/operational-cycle
COPY --from=builder /app/web/dist /app/web/dist

EXPOSE 8091
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8091/api/health || exit 1

ENTRYPOINT ["/app/operational-cycle"]
