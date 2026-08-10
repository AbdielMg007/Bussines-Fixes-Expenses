# Repository Guidelines

## Project Structure & Module Organization

Runway is a monorepo. Keep project-wide tooling and documentation at the root, and place implementation in its owning application:

- `backend/cmd/api/` contains the Go API entry point.
- `backend/internal/` contains private Go packages; `backend/tests/` contains cross-package tests.
- `frontend/app/` contains the Next.js App Router application.
- `docs/architecture/` and `docs/adr/` contain binding domain rules and decisions.
- `api/`, `infra/`, and `scripts/` are reserved for contracts, infrastructure, and repeatable tooling.

Do not duplicate financial rules in the frontend. The future deterministic engine belongs in the backend and must conform to the approved architecture documents.

## Build, Test, and Development Commands

Run commands from the repository root:

- `make backend-run` starts the API on `127.0.0.1:8080`.
- `make backend-test` runs all Go tests.
- `make frontend-dev` starts Next.js locally.
- `make frontend-test` performs the current frontend type-check test.
- `make frontend-build` creates a local frontend build.
- `make db-up` and `make db-down` manage local PostgreSQL after `.env` is created.

Install frontend packages with `npm --prefix frontend install`. Validate Compose with `make db-config`.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use tabs where Go requires them. TypeScript is strict and uses two-space indentation. Use lowercase Go package names, `PascalCase` for exported Go identifiers and React components, and descriptive `kebab-case` names for general files. Keep modules small and avoid adding abstractions before a concrete use case requires them.

## Testing Guidelines

Name Go tests `*_test.go` and test observable behavior with the standard library. The frontend currently uses TypeScript checking as its minimal test gate; add a browser or component test framework only when real UI behavior justifies it. Financial logic will require deterministic table-driven tests when introduced. Do not merge failing tests.

## Commit & Pull Request Guidelines

There is no Git history from which to infer an existing convention. Use concise, imperative commit subjects, preferably Conventional Commits such as `feat: add expense category validation` or `fix: handle missing receipt dates`.

Pull requests should explain the problem and solution, list verification performed, and link relevant issues. Include screenshots for visible UI changes and call out migrations, configuration changes, or follow-up work. Keep each pull request narrowly scoped and ensure generated files or secrets are not committed.
