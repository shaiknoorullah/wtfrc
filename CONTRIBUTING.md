# Contributing to wtfrc

Thanks for wanting to help. Here's how to get started.

## Development Setup

```bash
git clone https://github.com/shaiknoorullah/wtfrc.git
cd wtfrc
go mod tidy
make build
make test
```

Requires Go 1.24+ and Ollama for integration testing.

## Branch Strategy

- `main` — stable releases only
- `develop` — integration branch, nightly builds
- Feature branches — branch from `develop`, PR back to `develop`

```
feature/my-thing → develop → main
```

## Making Changes

1. Fork and clone
2. Branch from `develop`: `git checkout -b feat/my-thing develop`
3. Write tests first (TDD is how this project was built)
4. Make your changes
5. Run `make test` and `make lint`
6. Commit with [conventional commits](https://www.conventionalcommits.org/):
   - `feat:` new feature
   - `fix:` bug fix
   - `docs:` documentation
   - `refactor:` code change that neither fixes a bug nor adds a feature
   - `test:` adding or fixing tests
   - `ci:` CI/CD changes
   - `chore:` maintenance
7. Push and open a PR against `develop`

## Adding a Parser

Parsers live in `internal/indexer/parsers/`. To add one:

1. Create `yourparser.go` and `yourparser_test.go`
2. Implement the `Parser` interface (Name, CanParse, Parse)
3. Register via `init()` with `Register(&YourParser{})`
4. Write tests with temp files — see existing parsers for patterns

## Code Style

- `go vet` and `golangci-lint` must pass
- No unnecessary abstractions — three similar lines beat a premature helper
- Tests are not optional
- Comments explain *why*, not *what*

## Reporting Bugs

Use the [bug report template](https://github.com/shaiknoorullah/wtfrc/issues/new?template=bug_report.md). Include:
- `wtfrc doctor` output
- OS and GPU info
- Steps to reproduce

## Feature Requests

Open an issue with the [feature request template](https://github.com/shaiknoorullah/wtfrc/issues/new?template=feature_request.md). Explain the problem before the solution.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
