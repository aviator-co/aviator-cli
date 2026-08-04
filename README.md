# aviator-cli

`aviator` is Aviator's CLI for submitting verifications and creating runbooks
over the Aviator REST API.

## Install

With Homebrew:

```bash
brew install aviator-co/tap/aviator
```

With Go:

```bash
go install github.com/aviator-co/aviator-cli/cmd/aviator@latest
```

Binaries for Linux, macOS, and Windows are also attached to every
[release](https://github.com/aviator-co/aviator-cli/releases).

## Configuration

The CLI reads configuration from (first match wins):

- `$XDG_CONFIG_HOME/aviator/config.yaml`
- `$HOME/.config/aviator/config.yaml`
- `$HOME/.aviator/config.yaml`
- a repo-local `<git-common-dir>/aviator/config.yaml` (merged on top)

```yaml
aviator:
  apiHost: https://api.aviator.co   # override for on-prem
  apiToken: <your-api-token>
```

Environment variables override the config file:

- `AVIATOR_API_TOKEN`
- `AVIATOR_API_HOST`

## Usage

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
`--spec` are optional.

### Create a runbook

```bash
aviator runbook \
  --repo acme/web \
  --prompt "Migrate the settings page to the new design system" \
  --oneshot
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development and release setup.

## License

[MIT](./LICENSE)
