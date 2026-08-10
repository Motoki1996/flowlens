# FlowLens developer commands.
# Most targets load variables from .env when present.
SHELL := /bin/bash

# A makefile assignment beats an environment variable in GNU Make, so plain
# `-include .env` would let the .env value (host port 55432) shadow the
# DATABASE_URL the devcontainer exports (db:5432). Snapshot the environment
# first and restore it after the include, so the precedence is:
#   command line > environment > .env > default below.
ENV_DATABASE_URL := $(DATABASE_URL)
-include .env
export
ifneq ($(ENV_DATABASE_URL),)
DATABASE_URL := $(ENV_DATABASE_URL)
endif

API_DIR := apps/api
WEB_DIR := apps/web
MIGRATIONS := $(API_DIR)/migrations
DATABASE_URL ?= postgres://flowlens:flowlens@localhost:55432/flowlens?sslmode=disable

.PHONY: help
help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: setup
setup: ## Install dependencies for api and web, and create .env if missing.
	@test -f .env || (cp .env.example .env && echo "Created .env from .env.example")
	cd $(API_DIR) && go mod download
	cd $(WEB_DIR) && npm install

.PHONY: dev
dev: ## Start the full stack (Postgres + API + Web) with hot reload.
	docker compose up --build

.PHONY: down
down: ## Stop the stack.
	docker compose down

.PHONY: dev-container
dev-container: ## Start API + Web natively inside the devcontainer (no Docker; "db" service must already be running).
	@.devcontainer/dev.sh

.PHONY: migrate
migrate: ## Apply all up migrations against DATABASE_URL.
	migrate -path $(MIGRATIONS) -database "$(DATABASE_URL)" up

.PHONY: migrate-down
migrate-down: ## Roll back the last migration.
	migrate -path $(MIGRATIONS) -database "$(DATABASE_URL)" down 1

.PHONY: migrate-create
migrate-create: ## Create a new migration: make migrate-create name=add_x
	migrate create -ext sql -dir $(MIGRATIONS) -seq $(name)

.PHONY: generate
generate: ## Generate type-safe DB code from SQL (sqlc).
	cd $(API_DIR) && sqlc generate

.PHONY: test
test: ## Run api and web unit tests.
	cd $(API_DIR) && go test ./...
	cd $(WEB_DIR) && npm test -- --run

.PHONY: test-integration
test-integration: ## Run api integration tests (requires a running Postgres).
	cd $(API_DIR) && go test -tags=integration ./...

.PHONY: test-e2e
test-e2e: ## Run Playwright browser e2e tests (requires a running, migrated Postgres; starts its own api+web).
	cd $(WEB_DIR) && npx playwright test

.PHONY: lint
lint: ## Lint api (golangci-lint) and web (eslint).
	cd $(API_DIR) && golangci-lint run ./...
	cd $(WEB_DIR) && npm run lint

.PHONY: build
build: ## Build the api binary and the web app.
	cd $(API_DIR) && go build -o bin/api ./cmd/api
	cd $(WEB_DIR) && npm run build

.PHONY: build-images
build-images: ## Build the production Docker images for api and web.
	docker build -t flowlens-api:latest --target runtime $(API_DIR)
	docker build -t flowlens-web:latest --target runner \
		--build-arg NEXT_PUBLIC_API_BASE_URL=$(NEXT_PUBLIC_API_BASE_URL) \
		$(WEB_DIR)

.PHONY: storybook
storybook: ## Start Storybook for the web app (http://localhost:6006).
	cd $(WEB_DIR) && npm run storybook
