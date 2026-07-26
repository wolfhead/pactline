DSN ?= postgres://bounty:bounty@localhost:5433/bountyboard?sslmode=disable

up:
	docker compose up -d --wait

down:
	docker compose down -v

test: up
	DATABASE_URL="$(DSN)" go test ./... -count=1

run: up
	DATABASE_URL="$(DSN)" go run ./cmd/server

.PHONY: up down test run
