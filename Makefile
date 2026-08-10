.PHONY: backend-run backend-test frontend-dev frontend-test frontend-build db-up db-down db-config

backend-run:
	cd backend && go run ./cmd/api

backend-test:
	cd backend && go test ./...

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
