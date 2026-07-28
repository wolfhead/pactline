DSN ?= postgres://bounty:bounty@localhost:5433/bountyboard?sslmode=disable

up:
	docker compose up -d --wait

down:
	docker compose down -v

test: up
	DATABASE_URL="$(DSN)" go test ./... -count=1 -p 1

run: up
	APP_ENV=development AUTH_PROVIDER=development APP_BASE_URL=http://localhost:5173 \
	SESSION_SECRET=ZGV2ZWxvcG1lbnQtc2Vzc2lvbi1zZWNyZXQtMzJieXQ= \
	DATABASE_URL="$(DSN)" go run ./cmd/server

web-install:
	cd web && npm install

web-dev:
	cd web && VITE_AUTH_PROVIDER=development npm run dev

web-test:
	cd web && npm test

# e2e drives a real browser against the whole stack. Playwright's own
# webServer config brings up Postgres (via docker compose), the Go backend
# and the Vite dev server as needed, and reuses them if a developer already
# has them running — see web/playwright.config.ts.
e2e:
	cd web && npx playwright test

.PHONY: up down test run web-install web-dev web-test e2e
