# Runway

Runway is a personal financial control center designed to answer: “How much money can I safely spend without compromising upcoming obligations?” The financial engine will be deterministic; AI will eventually be limited to validated interface assistance.

## Current architecture

This repository is a simple monorepo:

- `backend/`: Go REST API with owner authentication and server-side sessions.
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

PostgreSQL is bound to `127.0.0.1` and stores data in the `postgres_data` Docker volume. Apply the versioned migrations after starting it:

```sh
make db-migrate
```

Stop PostgreSQL with:

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

Database integration tests use isolated PostgreSQL schemas and require the local database:

```sh
make backend-test-db
```

## Owner authentication

Run the migration, then create the single owner from a terminal:

```sh
make owner-bootstrap EMAIL=owner@example.com
```

The command prompts for and confirms the password without echoing it. It has no HTTP equivalent, and the database rejects every attempt to create a second owner. For non-interactive local automation, the bootstrap command also accepts `-password-stdin` when run from `backend/` with `DATABASE_URL` exported.

Authentication uses an `HttpOnly`, `SameSite=Strict` server-side session cookie. `SESSION_COOKIE_SECURE` defaults to `true`; the example disables it only for local HTTP. `SESSION_DURATION` is bounded between one minute and 30 days. `APP_ORIGIN` is required and protects state-changing cookie-authenticated requests with an exact `Origin` check.

After starting the API, a local authentication round trip can be tested with:

```sh
curl -sS -c /tmp/runway-cookies.txt \
  -H 'Origin: http://127.0.0.1:3000' \
  -H 'Content-Type: application/json' \
  --data '{"email":"owner@example.com","password":"your local password"}' \
  http://127.0.0.1:8080/api/v1/auth/login

curl -sS -b /tmp/runway-cookies.txt \
  http://127.0.0.1:8080/api/v1/auth/me

curl -sS -b /tmp/runway-cookies.txt \
  -H 'Origin: http://127.0.0.1:3000' \
  -X POST http://127.0.0.1:8080/api/v1/auth/logout
```

Required backend settings are `DATABASE_URL` and `APP_ORIGIN`. Optional authentication settings are `SESSION_DURATION` and `SESSION_COOKIE_SECURE`; safe local examples are documented in `.env.example`. Never commit `.env` or an owner password.

## Frontend

Install dependencies once:

```sh
npm --prefix frontend install
```

Start the development server:

```sh
make frontend-dev
```

Open `http://127.0.0.1:3000` so its site matches the local API cookie host. Type-check the frontend with `make frontend-test` and create a production build locally with `make frontend-build`. No browser testing framework has been added at this stage.

## Basic ledger API

All ledger endpoints require the session cookie created by login. State-changing requests also require the configured `Origin`. Transaction, snapshot, and transfer POSTs require an `Idempotency-Key` (1-128 characters using letters, digits, `.`, `_`, `:`, or `-`); retry the same request with the same key to receive the original logical result without posting twice. Monetary magnitudes use integer minor units; for example, `125050` means MXN 1,250.50.

Create an account:

```sh
curl -sS -b /tmp/runway-cookies.txt \
  -H 'Origin: http://127.0.0.1:3000' -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: example-transaction-001' \
  --data '{"name":"Primary bank","type":"bank","currency":"MXN"}' \
  http://127.0.0.1:8080/api/v1/accounts
```

Post an asset outflow and read the reconstructed balance:

```sh
curl -sS -b /tmp/runway-cookies.txt \
  -H 'Origin: http://127.0.0.1:3000' -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: example-transfer-001' \
  --data '{"effect":"asset_outflow","amount_minor":125050,"currency":"MXN","financial_date":"2026-08-10","memo":"Example purchase"}' \
  http://127.0.0.1:8080/api/v1/accounts/ACCOUNT_ID/transactions

curl -sS -b /tmp/runway-cookies.txt \
  http://127.0.0.1:8080/api/v1/accounts/ACCOUNT_ID/balance
```

Create an atomic same-currency transfer (use a credit-card account as the destination for a card payment):

```sh
curl -sS -b /tmp/runway-cookies.txt \
  -H 'Origin: http://127.0.0.1:3000' -H 'Content-Type: application/json' \
  --data '{"source_account_id":"BANK_ID","destination_account_id":"CASH_OR_CARD_ID","amount_minor":50000,"currency":"MXN","financial_date":"2026-08-10","memo":"Internal transfer"}' \
  http://127.0.0.1:8080/api/v1/transfers
```

The API also supports listing and archiving accounts, listing immutable posted transactions, and creating reconciled balance snapshots. There are no edit or delete transaction endpoints.

## Future commitments API

Issue 6 adds owner-scoped obligations, manual scheduled cash flows, and receivables. Their mutating endpoints require the same `Origin` and `Idempotency-Key` headers as ledger mutations. Obligation occurrences are derived on demand and are never persisted as scheduled-flow rows in Issue 6:

```sh
curl -sS -b /tmp/runway-cookies.txt \
  -H 'Origin: http://127.0.0.1:3000' -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: car-obligation-001' \
  --data '{"name":"Car payment","amount_minor":226400,"currency":"MXN","recurrence":"biweekly","start_date":"2026-08-16","end_date":null}' \
  http://127.0.0.1:8080/api/v1/obligations

curl -sS -b /tmp/runway-cookies.txt \
  'http://127.0.0.1:8080/api/v1/obligations/OBLIGATION_ID/occurrences?from=2026-08-01&to=2026-09-30'

curl -sS -b /tmp/runway-cookies.txt \
  -H 'Origin: http://127.0.0.1:3000' -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: archive-car-001' \
  --data '{"inactive_from":"2026-08-20"}' \
  http://127.0.0.1:8080/api/v1/obligations/OBLIGATION_ID/archive
```

`inactive_from` is the first suppressed financial date: earlier occurrences remain reproducible after archival. Expansion returns at most 1,000 occurrences and rejects larger requests. Monthly obligations retain their original day and clamp only in shorter months, so January 31 expands to February 28/29 and then March 31. Projection code must derive obligation occurrences directly and must not also load persisted obligation flows. An undated receivable is created with `"expected_date": null`; no date is inferred. Recording a collection updates receivable progress but does not automatically post a ledger transaction. An optional compatible ledger inflow ID may link the two records explicitly.

## Project status

Issues 5 and 6 provide the owner-scoped posted ledger plus future obligations, scheduled cash flows, and receivables. ProjectionPolicy, forecasting, safe-to-spend calculations, credit-card statements, MSI, AI integration, CI/CD, and production deployment are **not implemented**.

Implemented and future financial behavior must conform to [the financial domain](docs/architecture/financial-domain.md) and [ADR 0001](docs/adr/0001-financial-core-invariants.md).
