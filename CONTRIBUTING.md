# Contributing

Contributions are welcome! Here's how to get started.

## Development Setup

1. Fork and clone the repository
2. Install Go 1.23+ and PostgreSQL 16+
3. Copy config: `cp config.example.yaml config.yaml`
4. Start PostgreSQL and run: `make run`

## Guidelines

- Run `make test` and `make vet` before submitting
- Keep PRs focused on a single change
- Add tests for new functionality
- Follow existing code style

## Reporting Issues

Open an issue with:
- What you expected vs what happened
- Steps to reproduce
- Go version and OS

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
