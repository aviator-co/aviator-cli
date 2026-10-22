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
  show.go           # `aviator show`    -> runbook detail + its verification runs
  sessions.go       # `aviator sessions`-> list/lookup sessions by branch or PR
  results.go        # `aviator results` -> runbook step results
  edit.go           # `aviator edit`    -> PATCH intent / acceptance criteria
  dismiss.go        # `aviator dismiss` -> delete criteria / waive invariants
  scenarios.go      # `aviator scenarios` -> scenario runs + evidence of a verification run
  evidence.go       # `aviator evidence`  -> signed URL / download of one evidence file
  invariants.go     # `aviator invariants <list|categories|create|edit|delete|approve|reject|set-status>`
  invariants_format.go  # text rendering for the invariants commands
  actors.go         # `aviator actors <list|edit>` (experimental build only)
  decisions.go      # `aviator decisions <list|show|create|edit|comment|settle|reopen|apply>` (experimental)
  decisions_format.go  # text rendering for the decisions commands
  version.go        # `aviator version`
  helpers.go        # parseRepo(s), readSpecFile, collectCriteria, collectBody
internal/
  config/           # viper config load + version
  api/              # thin REST client (client.go) + per-resource methods
    credentials.go  # token source resolution (static token vs OAuth session)
  auth/             # OAuth login, keychain token store, refreshing TokenSource
  utils/colors/     # terminal color helpers
```

Commands are **flat**: `aviator verify` and `aviator runbook` are the actions
themselves — there is no second verb (no `submit`/`create` subcommand). The one
resource group is `aviator invariants`, whose subcommands are the CRUD verbs
over `/api/v1/invariants`.

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
- **`--json`**: every command builds its own JSON from a CLI-owned struct and
  never prints the backend body, so a backend change can't silently reshape
  what callers parse. Sessions are identified by `id` (`r/N`) and `url`
  (`sessionRef`); "runbook" stays out of Verify-facing keys.
- Code must be `gofumpt`-clean and pass `golangci-lint` (config in
  `.golangci.yaml`). CI (`.github/workflows/go.yml`) runs build, test, smoke
  test, and lint on every PR.
- gofumpt and goimports are `tool` dependencies in `go.mod`, so `just fmt`, the
  pre-commit hooks, and CI all run the same pinned binaries via `go tool`, and
  dependabot's gomod group keeps them current. `.golangci.yaml` intentionally has
  no `formatters` block: golangci-lint bundles its own older copies of both, and
  two copies grading the same files is how CI deadlocks.

## Experimental features

New features that are only for internal dogfooding sit behind the
`experimental` Go build tag, so they are left out of stable binaries entirely.
Nobody can switch them on with a flag or env var.

- A new command goes in its own file that starts with `//go:build experimental`
  and registers itself: `func init() { rootCmd.AddCommand(fooCmd) }`. Don't
  list it in `main.go`.
- Mark a new top-level command `Hidden: true`, so even experimental builds
  leave it out of `--help`, `aviator help`, and shell completion. It still runs
  when called by name.
- A new flag or branch inside an existing command is gated on
  `config.Experimental` (a const, so the stable build drops the dead code).
- No release (stable or nightly) is built with the tag; both are public. The
  team builds its own binary with `just build-experimental` or
  `go run -tags experimental ./cmd/aviator`.
- CI builds, tests, and smoke-tests both variants; golangci-lint runs with the
  tag on.
- To ship a feature to everyone, delete the build tag or the `config.Experimental`
  check.

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
  working_branch?, target_branch?, spec_file?, preview_config_name?}`. Creates a
  verify-only runbook session, owned by the caller, seeded with the criteria.
  `preview_config_name` (`--preview`) names one of the repo's preview configs;
  unknown names are a 400 `invalid-input`.
- `POST /api/v1/runbook` — `{repository, intent, oneshot, target_branch?,
  spec_file?, acceptance_criteria?, ...}`. Creates a runbook from an intent
  (`prompt` is a deprecated backend alias; the CLI sends `intent`); criteria
  are optional.
- `GET /api/v1/runbook/` — `?org=&repo=&working_branch=&status=&page=&per_page=`.
  Lists the caller's sessions in a repo, newest first, for `aviator sessions`.
  `working_branch` is the only filter, so `--pr` matches client-side over the
  `pull_requests` each summary carries.
- `PATCH /api/v1/verify/<n>` — `{intent?, acceptance_criteria?,
  expected_version?, preview_config_name?}`, at least one of intent, criteria,
  or preview config; `expected_version` is required with criteria (409
  `stale-runbook-version` when it's behind). Returns the session with its
  `url`, `version`, and `preview_config_name`. Starts no run.
- `POST /api/v1/verify/<n>/runs` — `{evaluator_only?, force?,
  preview_config_name?}`. Triggers a verification run, deduplicated against an
  equivalent run unless `force` or the preview config changes. A preview config
  set on submit, edit, or trigger sticks to the session for later runs.
- `POST /api/v1/verify/<n>/dismissals` — `{criteria: [{stable_key} |
  {baseline_invariant_id, category, justification}]}`. Deletes task criteria
  and waives invariants; the handles come from the detail endpoint. Starts no
  run.
- `GET /api/v1/verify/<n>/runs` — `?page=&per_page=`. The session's
  verification runs, newest first, for the run list in `aviator show`.
- `GET /api/v1/verify/<n>/runs/<run_id>` — one run with its failed criteria,
  for `aviator results --run`.
- `GET /api/v1/verify/<n>/runs/latest/scenarios` and
  `/runs/<run_id>/scenarios` — a run's scenario runs with their criteria
  handles and evidence ids. `aviator scenarios --run` picks the run.
- `GET /api/v1/verify/evidence/<id>` — 302 to a short-lived signed storage URL.
  The CLI doesn't follow it with the bearer token: `EvidenceURL` stops at the
  redirect and the download is a plain unauthenticated GET.

- `GET /api/v1/verify/actors` — `{actors: [{id, name, role}]}`, the caller's
  actors, for `aviator actors list`. Experimental build only. (The backend's
  find-or-create `POST` exists but the CLI doesn't call it.) The local
  `~/.config/aviator/actors.yaml` (`{actors: [{name, role, description}]}`,
  agents only) is edited by hand or by `aviator actors edit`, never sent to
  the API, and reconciled by the server.

- `/api/v1/verify/<n>/decisions` — experimental build only. Every body
  rejects unknown keys, and every actor is `{role, name}` on the wire (found or
  created by the server), but a bare name in batch files and `--actor`, which
  the CLI resolves against the local actors file before sending. People never
  go in that file: the built-in actor `person` is the signed-in user, sent as
  `{role: "person"}` with no name, and the server names it after the Aviator
  user behind the token.
  - `GET` — `{decisions: [...]}`, open first. `GET /<id>` — one decision.
  - `POST` — `{decisions: [{type, title, actor, summary?, anchor?,
    outcome?}]}`, creates only, all-or-nothing, in request order. An `outcome`
    creates it settled.
  - `PATCH /<id>` — `{actor, type?, title?, summary?, anchor?, outcome?}`;
    only keys present change, and `outcome` settles. `outcome: null` reopens a
    settled decision, the only change a settled one accepts (409 otherwise).
  - `POST /<id>/comments` — `{via: "aviator-cli", model_metadata?: {model},
    comments: [{actor, body}]}`, all-or-nothing. `via` and `model_metadata`
    live only here. `PATCH /<id>/comments/<comment_id>` — `{body}`.
  - Permission is on the user behind the token, not the actor: only the user
    who raised a decision can patch it, and only the comment's writer can edit
    it (403). Settling writes no comment, so a reason is a separate comments
    POST.
  - `aviator decisions apply` keeps the CLI's own batch file shape
    (`{model_metadata?, decisions: [{decision: {id?, ...}, comments?,
    settlement?: {outcome, actor, body?}}]}`) and `applyBatch` splits it: one
    create POST (settled ones with their outcome), a PATCH per existing
    decision changed or settled, then a comments POST per decision, with the
    settlement body last as a comment from its actor. That's not atomic: a
    refusal stops the rest and the error lists what was already recorded. The
    single-decision commands build the same batch.

`POST` and `PATCH /api/v1/verify`, dismissals, and the listing are gated on
`role="user"`: an account-scoped API token resolves to no role and gets a 403,
so they need a user token or an `aviator login` session.

- `/api/v1/invariants` — `GET` (list; `org`+`repo`, `status`, comma-separated
  `ids` and `source`, `page`, `per_page` query), `GET /categories`, `POST`
  (create), `PATCH /<id>` (partial update; `repositories` and `conditions`
  replace wholesale when present, `[]` clears; `enabled` is refused unless the
  invariant is active), `DELETE /<id>`, and `POST /status`
  (`{invariant_ids, status}` bulk approve/reject/restore). Reads need any
  member; writes need a maintainer.

When changing a request/response shape, keep it in sync with the backend
schemas (`src/api/verify.py`, `src/api/runbook.py`, `src/api/invariants.py` in
the mergeit repo).

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
