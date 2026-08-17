DSN ?= postgres://bounty:bounty@localhost:5433/bountyboard?sslmode=disable
COMPOSE_PROJECT_NAME ?= task_manager
TEST_COMPOSE_FILE ?= compose.test.yaml
STACK_COMPOSE_FILE ?= compose.yaml

up:
	docker compose -p "$(COMPOSE_PROJECT_NAME)" -f "$(TEST_COMPOSE_FILE)" up -d --wait

down:
	docker compose -p "$(COMPOSE_PROJECT_NAME)" -f "$(TEST_COMPOSE_FILE)" down -v

test: up
	DATABASE_URL="$(DSN)" go test ./... -count=1 -p 1

run: up
	APP_ENV=development AUTH_PROVIDER=development APP_BASE_URL=http://localhost:5173 \
	SESSION_SECRET=ZGV2ZWxvcG1lbnQtc2Vzc2lvbi1zZWNyZXQtMzJieXQ= \
	RABBITMQ_URL=amqp://guest:guest@localhost:5673/ \
	DATABASE_URL="$(DSN)" go run ./cmd/server

web-install:
	cd web && npm install

web-dev:
	cd web && VITE_AUTH_PROVIDER=development npm run dev

web-test:
	cd web && npm test

web-build:
	cd web && npm run build

stack-up:
	docker compose -f "$(STACK_COMPOSE_FILE)" up -d --build --wait

stack-down:
	docker compose -f "$(STACK_COMPOSE_FILE)" down

stack-logs:
	docker compose -f "$(STACK_COMPOSE_FILE)" logs -f api web

openapi-generate:
	go generate ./api

openapi-check:
	go generate ./api
	git diff --exit-code -- api/openapi.yaml internal/api/v1generated

pactline-cli:
	CGO_ENABLED=0 go build -trimpath \
		-ldflags="-X github.com/wolfhead/pactline/internal/cli.Version=0.1.0-dev" \
		-o bin/pactline ./cmd/pactline

pactline-fleet-install:
	npm --prefix fleet ci

pactline-fleet-deepseek-install:
	npm --prefix fleet/runtime/deepseek ci

pactline-fleet-codex-install:
	npm --prefix fleet/runtime/codex ci --ignore-scripts

pactline-fleet-codex-test:
	npm --prefix fleet ci
	npm --prefix fleet run codex:test
	npm --prefix fleet run build
	node fleet/lib/bin.js codex-doctor --json

pactline-fleet-codex-l1:
	npm --prefix fleet ci
	npm --prefix fleet/runtime/codex ci --ignore-scripts
	npm --prefix fleet run codex:l1

pactline-fleet-deepseek-test:
	npm --prefix fleet ci
	npm --prefix fleet run deepseek:test
	npm --prefix fleet run build
	node fleet/lib/bin.js deepseek-doctor --json

pactline-fleet-deepseek-l1:
	npm --prefix fleet ci
	npm --prefix fleet/runtime/deepseek ci
	npm --prefix fleet run deepseek:l1

pactline-fleet-check:
	npm --prefix fleet ci
	npm --prefix fleet run typecheck
	npm --prefix fleet test
	npm --prefix fleet run build
	npm --prefix fleet run test:e2e
	node fleet/scripts/m5-1-smoke.mjs
	node fleet/scripts/m5-3-package-smoke.mjs

pactline-fleet-doctor: pactline-cli
	npm --prefix fleet ci
	npm --prefix fleet run build
	node fleet/lib/bin.js doctor --json --pactline "$(CURDIR)/bin/pactline"

pactline-fleet-local-integration: stack-up pactline-cli
	npm --prefix fleet ci
	PACTLINE_FLEET_PACTLINE_BIN="$(CURDIR)/bin/pactline" npm --prefix fleet run local:integration

pactline-release:
	sh scripts/build-pactline-release.sh

# e2e drives a real browser against the whole stack. Playwright's own
# webServer config brings up Postgres (via docker compose), the Go backend
# and the Vite dev server as needed, and reuses them if a developer already
# has them running — see web/playwright.config.ts.
e2e:
	cd web && npx playwright test

agent-api-e2e:
	cd web && npx playwright test e2e/26-agent-api.spec.ts

agent-eval:
	go run ./cmd/agent-eval --scenario all --judge=true --format markdown

.PHONY: up down test run web-install web-dev web-test web-build stack-up stack-down stack-logs openapi-generate openapi-check pactline-cli pactline-fleet-install pactline-fleet-deepseek-install pactline-fleet-deepseek-test pactline-fleet-deepseek-l1 pactline-fleet-codex-install pactline-fleet-codex-test pactline-fleet-codex-l1 pactline-fleet-check pactline-fleet-doctor pactline-fleet-local-integration pactline-release e2e agent-api-e2e agent-eval
