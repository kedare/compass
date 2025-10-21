# Repository Guidelines

## Project Structure & Module Organization
- `main.go` wires the CLI entry point and pulls in Cobra commands.
- `cmd/` groups user-facing commands (`connectivity_*.go`, `gcp.go`, `root.go`) plus integration tests in `gcp_test.go`.
- `internal/` holds reusable services: `gcp/` for API clients, `ssh/` for tunneling, `output/` for formatting, `logger/` for structured logs, and `cache/` for local state.
- `Taskfile.yml` centralizes build/test tasks; `CONNECTIVITY_TESTS.md` documents the connectivity features.
- Built binaries (e.g., `./compass`) live in the repo root—delete them before committing if you generate new ones.
- All functions should be compatible with both IPv4 and IPv6.

## Build, Test, and Development Commands
- `task build` compiles the `compass` binary with version metadata; `task build-all` cross-compiles for common targets.
- `task run -- gcp <instance> --project <id>` runs the CLI without writing a binary.
- `task fmt`, `task lint`, and `task vet` enforce formatting, linting, and static checks.
- `task test`, `task test-short`, and `task test-integration` cover full, unit, and integration suites; add `task test-coverage` when you need HTML or function coverage reports.
- `task dev` expects `air`; use it for rapid rebuilds while iterating on command handlers.
- To build in the sandbox, build into .gocache/, no need to clear if before/after each task.

## Coding Style & Naming Conventions
- Always run `go fmt ./...` (or `task fmt`) before sending changes; tabs and import grouping follow Go defaults.
- Lint locally with `golangci-lint run ./...`; prefer fixing warnings rather than suppressing them.
- Package names stay lower_snake (`internal/output`); exported symbols use PascalCase and include doc comments when non-obvious.
- CLI command files follow `connectivity_<verb>.go`; keep Cobra command names kebab-case (e.g., `connectivity-test`).
- Make sure ALL declared functions and type have a proper up to date godoc declared, keep them updated
- Maintain the README.md updated with all new changes of interface and details about the inner working.
- All terminal related rendering and UI should be done (when possible) using features from `github.com/pterm/pterm`, incluiding logging.

## Testing Guidelines
- Place table-driven unit tests alongside implementation files as `<name>_test.go`.
- Use `task test-short` for quick feedback; rely on `task test` before merging connectivity or network features.
- Track coverage trends with `task test-coverage-func`; flag drops below existing package baselines during review.
- For network-dependent tests, guard with `testing.Short()` or dedicated build tags to keep CI deterministic.

## Commit & Pull Request Guidelines
- Follow the concise, imperative style seen in history (`git commit -m "Improve connectivity tests"`); group logical changes per commit.
- Open PRs with a summary, testing notes (`task test`, `task lint` outputs), and links to relevant issues or tickets.
- Include CLI usage snippets or screenshots when UI/UX behavior changes, and confirm that generated artifacts (binaries, coverage files) are excluded before pushing.

## Security & Configuration Tips
- Rely on your local `gcloud` login; never embed service account keys or project IDs meant to stay private.
- Double-check command examples for redacted project names before publishing docs or PR descriptions.
