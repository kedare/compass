# Repository Guidelines

## Project Structure & Module Organization
- `main.go` wires the CLI entry point and pulls in Cobra commands.
- `cmd/` groups user-facing commands (`connectivity_*.go`, `gcp.go`, `root.go`) plus integration tests in `gcp_test.go`.
- `internal/` holds reusable services: `gcp/` for API clients, `ssh/` for tunneling, `output/` for formatting, `logger/` for structured logs, and `cache/` for local state.
- `Taskfile.yml` centralizes build/test tasks; `CONNECTIVITY_TESTS.md` documents the connectivity features.
- Built binaries (e.g., `./compass`) live in the repo root—delete them before committing if you generate new ones.

## Build, Test, and Development Commands
- `task build` compiles the `compass` binary with version metadata; `task build-all` cross-compiles for common targets.
- `task run -- gcp <instance> --project <id>` runs the CLI without writing a binary.
- `task fmt`, `task lint`, and `task vet` enforce formatting, linting, and static checks.
- `task test`, `task test-short`, and `task test-integration` cover full, unit, and integration suites; add `task test-coverage` when you need HTML or function coverage reports.
- `task dev` expects `air`; use it for rapid rebuilds while iterating on command handlers.

## Coding Style & Naming Conventions
- Always run `go fmt ./...` (or `task fmt`) before sending changes; tabs and import grouping follow Go defaults.
- Lint locally with `golangci-lint run ./...`; prefer fixing warnings rather than suppressing them.
- Package names stay lower_snake (`internal/output`); exported symbols use PascalCase and include doc comments when non-obvious.
- CLI command files follow `connectivity_<verb>.go`; keep Cobra command names kebab-case (e.g., `connectivity-test`).

## Testing Guidelines
- Place table-driven unit tests alongside implementation files as `<name>_test.go`.
- Use `task test-short` for quick feedback; rely on `task test-integration` before merging connectivity or network features.
- Track coverage trends with `task test-coverage-func`; flag drops below existing package baselines during review.
- For network-dependent tests, guard with `testing.Short()` or dedicated build tags to keep CI deterministic.

## Commit & Pull Request Guidelines
- Follow the concise, imperative style seen in history (`git commit -m "Improve connectivity tests"`); group logical changes per commit.
- Open PRs with a summary, testing notes (`task test`, `task lint` outputs), and links to relevant issues or tickets.
- Include CLI usage snippets or screenshots when UI/UX behavior changes, and confirm that generated artifacts (binaries, coverage files) are excluded before pushing.

## Security & Configuration Tips
- Rely on your local `gcloud` login; never embed service account keys or project IDs meant to stay private.
- Double-check command examples for redacted project names before publishing docs or PR descriptions.
