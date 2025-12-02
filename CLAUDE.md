# gosoulseek - Go Port of Soulseek.NET

## Dev Workflow

```bash
make fmt           # Format code (goimports-reviser)
make lint          # Run golangci-lint
make test          # Run tests
make coverage      # Run tests with coverage
make check         # Format + lint + test
make build         # Verify compilation (no binary output)
make install-hooks # Install git pre-commit hook
```

Run `make install-hooks` after cloning. Pre-commit hook runs `make check` before each commit.

## Git Workflow

Always wait for user confirmation before committing or pushing changes.

## Project Goal

Port the Soulseek.NET library (https://github.com/jpdillingham/Soulseek.NET) to Go. The reference .NET project is cloned in `Soulseek.NET/` (gitignored).

## Reference Project Structure

The Soulseek.NET project is located in `Soulseek.NET/` for inspection during porting.
