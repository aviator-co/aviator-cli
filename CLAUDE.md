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
  login.go          # `aviator login`   -> browser OAuth flow
  logout.go         # `aviator logout`  -> drop the stored session
  verify.go         # `aviator verify`  -> POST /api/v1/verify
  runbook.go        # `aviator runbook` -> POST /api/v1/runbook
  show.go           # `aviator show`    -> runbook detail
  sessions.go       # `aviator sessions`-> list/lookup sessions by branch or PR
  results.go        # `aviator results` -> runbook step results
  edit.go           # `aviator edit`    -> PATCH acceptance criteria
  version.go        # `aviator version`
  helpers.go        # parseRepo, readSpecFile, collectCriteria
internal/
  config/           # viper config load + version
  api/              # thin REST client (client.go) + per-resource methods
    credentials.go  # token source resolution (static token vs OAuth session)
  auth/             # OAuth login, keychain token store, refreshing TokenSource
  utils/colors/     # terminal color helpers
```

Commands are **flat**: `aviator verify` and `aviator runbook` are the actions
themselves — there is no second verb (no `submit`/`create` subcommand).

## Commands

A `justfile` wraps the common Go commands:

```bash
just build         # go build -o aviator ./cmd/aviator
just test          # go test --vet=all ./...
just lint          # format check + golangci-lint run (must be installed)
just fmt           # gofumpt + goimports, rewriting in place
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
- **Credentials**: `internal/auth` owns the OAuth flow and the keychain; the
  API client only sees a `TokenSource`. Precedence is `AVIATOR_API_TOKEN`, then
  the config file's `apiToken`, then the keychain session from `aviator login`.
  Tokens are never written to files.
- **Output**: use `internal/utils/colors` helpers; keep success output to a
  short confirmation line plus a couple of indented details.
- Code must be `gofumpt`-clean and pass `golangci-lint` (config in
  `.golangci.yaml`). CI (`.github/workflows/go.yml`) runs build, test, smoke
  test, and lint on every PR.
- gofumpt and goimports are `tool` dependencies in `go.mod`, so `just fmt`, the
  pre-commit hooks, and CI all run the same pinned binaries via `go tool`, and
  dependabot's gomod group keeps them current. `.golangci.yaml` intentionally has
  no `formatters` block: golangci-lint bundles its own older copies of both, and
  two copies grading the same files is how CI deadlocks.

## CI and releases

- `.github/workflows/go.yml` — build, test, smoke test, golangci-lint on PRs.
- `.github/workflows/pre-commit.yml` — goimports and gofumpt (both via
  `go tool`), end-of-file-fixer, JSON-schema validation of the workflow files,
  and zizmor (GitHub Actions security audit, configured by `.github/zizmor.yml`).
  Run the same set locally with `pre-commit run --all-files`.
- `.github/workflows/release.yml` — GoReleaser on tag push (or manual dispatch
  from Aviator's deploy tooling). Builds linux/darwin/windows × amd64/arm64,
  attaches archives + deb/rpm to a GitHub release, and pushes the `aviator`
  formula to `aviator-co/homebrew-tap`.
- `.github/workflows/nightly-release.yml` — prerelease builds tagged
  `v<version>-nightly`, publishing the `aviator-nightly` formula only.
- Stable releases publish the deb/rpm packages to fury.io via the
  `FURY_PUSH_TOKEN` secret; nightlies skip packaging entirely.
- Both release workflows post start/success/failure to Slack through the local
  `.github/actions/notify-deploy` composite action, using the
  `SLACK_WEBHOOK_PROD_DEPLOY_UPDATES` repo variable. It mirrors mergeit's action
  of the same name but is deliberately a separate copy, since mergeit does not
  share its actions outside that repo.
- Actions must stay ref-pinned (`actions/checkout@v7`) or hash-pinned with a
  matching version comment, or zizmor fails the build.

## Backend contract

The CLI targets endpoints in the `mergeit` backend:

- `POST /api/v1/verify` — `{repository:{org,name}, intent, acceptance_criteria,
  working_branch?, target_branch?, spec_file?}`. Creates a verify-only runbook
  session, owned by the caller, seeded with the criteria.
- `POST /api/v1/runbook` — `{repository, intent, oneshot, target_branch?,
  spec_file?, acceptance_criteria?, ...}`. Creates a runbook from an intent
  (`prompt` is a deprecated backend alias; the CLI sends `intent`); criteria
  are optional.
- `GET /api/v1/runbook/` — `?org=&repo=&working_branch=&status=&page=&per_page=`.
  Lists the caller's sessions in a repo, newest first, for `aviator sessions`.
  `working_branch` is the only filter, so `--pr` matches client-side over the
  `pull_requests` each summary carries.

`/api/v1/verify` and the listing are gated on `role="user"`: an account-scoped
API token resolves to no role and gets a 403, so both need a user token or an
`aviator login` session.

When changing a request/response shape, keep it in sync with the backend
schemas (`src/api/verify.py`, `src/api/runbook.py` in the mergeit repo).

`aviator login` runs an RFC 8414 discovery and an
authorization-code-with-PKCE-S256 flow against the Aviator OAuth server. The
CLI is a first-party public client: its `client_id` is the constant `clientID`
in `internal/auth/session.go`, it holds no client secret, and it registers
nothing at runtime. The callback listens on an ephemeral loopback port and the
redirect URI is built from it, which the server matches per RFC 8252 — it must
be `http://`, the literal `127.0.0.1`, path exactly `/callback`, and carry no
query or fragment.

Sessions live in the OS keychain, keyed per API host, and are never written to
disk. The access token is refreshed once it expires. Refresh tokens rotate and
the server revokes the whole family if one is reused, so refreshes are
serialized across concurrent invocations with a file lock
(`internal/auth/lock.go`).

## Git workflow

This repo is `av`-initialized. Use the `av` CLI for branches/PRs (the same
stacked-PR workflow as other Aviator repos). Don't commit the built binary
(`/aviator` is gitignored).
