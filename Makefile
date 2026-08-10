.PHONY: backend-run backend-test backend-test-db frontend-dev frontend-test frontend-build db-up db-down db-config db-migrate owner-bootstrap

backend-run:
	set -a; . ./.env; set +a; cd backend && go run ./cmd/api

backend-test:
	cd backend && go test ./...

backend-test-db:
	set -a; . ./.env; set +a; export TEST_DATABASE_URL="$$DATABASE_URL"; cd backend && go test ./internal/postgres -count=1

frontend-dev:
	npm --prefix frontend run dev

frontend-test:
	npm --prefix frontend test

frontend-build:
	npm --prefix frontend run build

db-up:
	docker compose --env-file .env up -d postgres

db-down:
	docker compose --env-file .env down

db-config:
	docker compose --env-file .env config

db-migrate:
	set -a; . ./.env; set +a; cd backend && go run ./cmd/migrate

owner-bootstrap:
	@test -n "$(EMAIL)" || (echo "usage: make owner-bootstrap EMAIL=owner@example.com"; exit 2)
	set -a; . ./.env; set +a; cd backend && go run ./cmd/bootstrap -email "$(EMAIL)"
