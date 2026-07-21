# FlowLens developer commands.
# Most targets load variables from .env when present.
SHELL := /bin/bash
-include .env
export

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

.PHONY: lint
lint: ## Lint api (golangci-lint) and web (eslint).
	cd $(API_DIR) && golangci-lint run ./...
	cd $(WEB_DIR) && npm run lint

.PHONY: build
build: ## Build the api binary and the web app.
	cd $(API_DIR) && go build -o bin/api ./cmd/api
	cd $(WEB_DIR) && npm run build
