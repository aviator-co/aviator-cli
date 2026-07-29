# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Overview

`aviator` is Aviator's command-line tool for submitting verifications and
creating runbooks against the Aviator **REST API**. It is a Go (Cobra) CLI that
deliberately mirrors the structure and conventions of the `av` CLI
(`github.com/aviator-co/av`) — but talks REST, not GraphQL.

Module: `github.com/aviator-co/aviator-cli` · Go 1.26.

## Layout

```
cmd/aviator/        # CLI entry point + commands (one file per command)
  main.go           # root command, PersistentPreRunE (config load), run()
  verify.go         # `aviator verify`  -> POST /api/v1/verify
  runbook.go        # `aviator runbook` -> POST /api/v1/runbook
  version.go        # `aviator version`
  helpers.go        # parseRepo, readSpecFile, collectCriteria
internal/
  config/           # viper config load + version
  api/              # thin REST client (client.go) + per-resource methods
  utils/colors/     # terminal color helpers
```

Commands are **flat**: `aviator verify` and `aviator runbook` are the actions
themselves — there is no second verb (no `submit`/`create` subcommand).

## Commands

A `justfile` wraps the common Go commands:

```bash
just build         # go build -o aviator ./cmd/aviator
just test          # go test --vet=all ./...
just lint          # golangci-lint run (must be installed)
just run -- ...     # go run ./cmd/aviator ...
just tidy          # go mod tidy
```

Equivalent raw commands: `go build ./...`, `go test --vet=all ./...`,
`go run ./cmd/aviator --help`.

## Conventions (match the `av` CLI)

- **Cobra** commands: package-level `xFlags` struct, command var with
  `Use/Short/Args/RunE`, flags registered in `init()`. `RunE` returns errors;
  `main.run()` prints them (commands set `SilenceErrors`/`SilenceUsage`).
- **Errors**: wrap with `emperror.dev/errors` (`errors.Wrap`, `errors.Errorf`,
  `errors.Sentinel`). Don't `fmt.Errorf` user-facing errors that need context.
- **Config**: `internal/config` loads (in order) `$XDG_CONFIG_HOME/aviator`,
  `$HOME/.config/aviator`, `$HOME/.aviator`, then a repo-local
  `<git-common-dir>/aviator/config.*` override. Env vars `AVIATOR_API_TOKEN` and
  `AVIATOR_API_HOST` override the file. Default host `https://api.aviator.co`.
- **API client**: all HTTP lives in `internal/api`. Add a new endpoint as its
  own file with a request/response struct pair and a method on `*Client`; reuse
  `Client.postJSON`. Bearer auth + the `{error, message}` error envelope are
  handled centrally.
- **Output**: use `internal/utils/colors` helpers; keep success output to a
  short confirmation line plus a couple of indented details.
- Code must be `gofmt`-clean and pass `golangci-lint` (config in
  `.golangci.yaml`). CI (`.github/workflows/go.yml`) runs build, test, smoke
  test, and lint on every PR.

## Backend contract

The CLI targets endpoints in the `mergeit` backend:

- `POST /api/v1/verify` — `{repository:{org,name}, intent, acceptance_criteria,
  branch_name?, spec_file?, author_email?}`. Creates a verify-only runbook
  session seeded with the criteria.
- `POST /api/v1/runbook` — `{repository, intent, oneshot, target_branch?,
  spec_file?, acceptance_criteria?, ...}`. Creates a runbook from an intent
  (`prompt` is a deprecated backend alias; the CLI sends `intent`); criteria
  are optional.

When changing a request/response shape, keep it in sync with the backend
schemas (`src/api/verify.py`, `src/api/runbook.py` in the mergeit repo).

## Git workflow

This repo is `av`-initialized. Use the `av` CLI for branches/PRs (the same
stacked-PR workflow as other Aviator repos). Don't commit the built binary
(`/aviator` is gitignored).
