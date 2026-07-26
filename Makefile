DSN ?= postgres://bounty:bounty@localhost:5433/bountyboard?sslmode=disable

up:
	docker compose up -d --wait

down:
	docker compose down -v

test: up
	DATABASE_URL="$(DSN)" go test ./... -count=1 -p 1

run: up
	DATABASE_URL="$(DSN)" go run ./cmd/server

web-install:
	cd web && npm install

web-dev:
	cd web && npm run dev

web-test:
	cd web && npm test

.PHONY: up down test run web-install web-dev web-test
