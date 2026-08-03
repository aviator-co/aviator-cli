# Development setup

Install the latest version of Go from https://go.dev/doc/install.

To run the command line:

```
go run ./cmd/aviator [subcommand/flags...]
```

Common tasks are wrapped in the [justfile](./justfile) (`just --list`).

Formatting and workflow linting run through
[pre-commit](https://pre-commit.com/). Install the hooks once with
`pre-commit install`; CI runs the same checks on every pull request.

gofumpt and goimports are `tool` dependencies in `go.mod`, so `just fmt`, the
pre-commit hooks and CI all run the same versions. `just lint` checks formatting
before handing off to golangci-lint.

# Release

To create a release, create a tag with the desired version and push to GitHub.

```
# Change the version as appropriate
TAG="v0.0.0"

git tag "$TAG"
git push origin tags/"$TAG"
```

This will automatically trigger [Goreleaser](https://goreleaser.com/) (as part
of the
[`release.yml` workflow](https://github.com/aviator-co/aviator-cli/blob/master/.github/workflows/release.yml))
which will create a GitHub release and publish the binaries to the
[Aviator Homebrew tap](https://github.com/aviator-co/homebrew-tap).

The deb/rpm packages are also published to fury.io.
