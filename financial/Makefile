.PHONY: run build test migrate

run:
go run ./cmd/api/main.go

build:
go build -o bin/api ./cmd/api

test:
go test ./... -v

migrate-up:
go run ./cmd/migrate up

migrate-down:
go run ./cmd/migrate down
