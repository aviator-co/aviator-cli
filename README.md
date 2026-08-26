# aviator-cli

`aviator` is Aviator's CLI for submitting verifications and creating runbooks
over the Aviator REST API.

## Install

With Homebrew:

```bash
brew trust aviator-co/tap  # Homebrew 6+ only loads trusted third-party taps
brew install aviator-co/tap/aviator
```

With Go:

```bash
go install github.com/aviator-co/aviator-cli/cmd/aviator@latest
```

Binaries for Linux, macOS, and Windows are also attached to every
[release](https://github.com/aviator-co/aviator-cli/releases).

## Authentication

Sign in through your browser:

```bash
aviator login
```

This runs an OAuth authorization-code flow with PKCE, briefly listening on a
loopback port for the browser to be redirected back to, and stores the session
in your OS keychain, refreshing it automatically. `aviator logout` removes the
stored session; Aviator has no token revocation endpoint, so a token that was
already issued stays valid until it expires.

For CI and other headless environments, set a static API token instead:

```bash
export AVIATOR_API_TOKEN=<your-api-token>
```

Credentials are used in this order: `AVIATOR_API_TOKEN`, then
`aviator.apiToken` from the config file, then the keychain session from
`aviator login`.

## Configuration

The CLI reads configuration from (first match wins):

- `$XDG_CONFIG_HOME/aviator/config.yaml`
- `$HOME/.config/aviator/config.yaml`
- `$HOME/.aviator/config.yaml`
- a repo-local `<git-common-dir>/aviator/config.yaml` (merged on top)

```yaml
aviator:
  apiHost: https://api.aviator.co   # override for on-prem
  apiToken: <your-api-token>        # optional; prefer `aviator login`
```

Environment variables override the config file:

- `AVIATOR_API_TOKEN`
- `AVIATOR_API_HOST`

## Usage

Two commands, two different jobs:

- `aviator verify`: you wrote the code, Aviator verifies the PR against your
  acceptance criteria.
- `aviator runbook`: Aviator's agent writes the code from your spec and opens
  its own PR.

### Submit for verification

```bash
aviator verify \
  --repo acme/web \
  --intent "Ensure the feature flag gates the new banner" \
  --criteria "Banner hidden when flag off" \
  --criteria "Banner shown when flag on" \
  --working-branch feature/banner \
  --target-branch main \
  --spec ./spec.md
```

`--criteria` is repeatable; alternatively pass `--criteria-file` (one criterion
per line, `#` comments ignored). `--working-branch`, `--target-branch`, and
`--spec` are optional, though without `--working-branch` the session can only
bind to a PR through a `Runbook: <url>` line in the PR body.

One verify session tracks exactly one PR. Stacked or multi-PR work needs one
submission per PR, each with its own `--working-branch`, intent, and criteria.
To update the criteria on a session that already exists, use `aviator edit`
rather than submitting the branch again.

Pass `--json` to print the submission as a single JSON object
(`runbook_number`, `runbook_id`, `url`, `working_branch`, `target_branch`,
`criteria_count`) instead of the human summary.

### Create a runbook

Aviator's agent implements the spec and opens its own PR. For code you wrote
yourself, use `aviator verify` instead.

```bash
aviator runbook \
  --repo acme/web \
  --intent "Migrate the settings page to the new design system" \
  --spec ./spec.md \
  --criteria "Settings page renders with the new components" \
  --target-branch main
```

`--intent` is required; `--title`, `--spec`, `--criteria`/`--criteria-file`,
`--target-branch`, and `--author-email` are optional. `--oneshot` is on by
default. `--json` prints `runbook_number`, `runbook_id`, `url`, `status`, and
`criteria_count` as a single JSON object.

### Inspect a session

```bash
aviator show r/123            # session summary
aviator results r/123         # latest verification results
aviator edit r/123 --expected-version 4 --criteria "..."
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development and release setup.

## License

[MIT](./LICENSE)
