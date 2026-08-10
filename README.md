# Runway

Runway is a personal financial control center designed to answer: “How much money can I safely spend without compromising upcoming obligations?” The financial engine will be deterministic; AI will eventually be limited to validated interface assistance.

## Current architecture

This repository is a simple monorepo:

- `backend/`: Go REST API. It currently exposes only `GET /health`.
- `frontend/`: Next.js App Router application written in TypeScript.
- `api/`: reserved for a future API contract.
- `docs/`: approved financial-domain rules and architecture decisions.
- `infra/`: reserved for future infrastructure configuration.
- `scripts/`: reserved for repeatable project scripts.
- `compose.yaml`: local PostgreSQL service only.

The application is a modular monolith, not a collection of microservices.

## Prerequisites

- Go 1.26 or newer.
- Node.js 20.9 or newer; Node.js 22 LTS is recommended.
- npm.
- Docker with Docker Compose v2.

## Local setup

Create local environment settings:

```sh
cp .env.example .env
```

The example credentials are for local development only. Do not reuse them outside a local machine or commit `.env`.

## PostgreSQL

Start the local database:

```sh
make db-up
```

PostgreSQL is bound to `127.0.0.1` and stores data in the `postgres_data` Docker volume. The application is not connected to it yet. Stop it with:

```sh
make db-down
```

## Backend

Run the API from the repository root:

```sh
make backend-run
```

It listens on `127.0.0.1:8080` by default. Override `HOST` or `PORT` when needed. Check health with:

```sh
curl http://127.0.0.1:8080/health
```

Run backend tests:

```sh
make backend-test
```

## Frontend

Install dependencies once:

```sh
npm --prefix frontend install
```

Start the development server:

```sh
make frontend-dev
```

Open `http://localhost:3000`. Type-check the frontend with `make frontend-test` and create a production build locally with `make frontend-build`. No browser testing framework has been added at this stage.

## Project status

Issue 2 establishes local tooling and testable application shells only. Financial accounts, transactions, cards, obligations, forecasting, authentication, database migrations, AI integration, CI/CD, and production deployment are **not implemented**.

Financial behavior must conform to [the financial domain](docs/architecture/financial-domain.md) and [ADR 0001](docs/adr/0001-financial-core-invariants.md) when implementation begins.
