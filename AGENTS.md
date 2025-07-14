# Codex Agent Guide for otail

## Repository Overview
- `cmd/`: contains the main application. `cmd/main.go` is the CLI entrypoint.
- `examples/`: standalone sample programs demonstrating Bubble Tea components. They are not built or tested with the main application.
- `opentelemetry-demo/`: Helm values for running the OpenTelemetry demo locally.

## Development Guidelines
1. Use Go 1.24 or newer.
2. Format all Go code with `gofmt -w` (or `go fmt ./...`).
3. Run `go vet ./...` and `go test ./...` after making changes. The project currently has no tests, but the command should still run and succeed.
4. Ensure the main application builds: `go build -o otail ./cmd`.
5. Keep example programs and Helm values unchanged unless your task explicitly requires modifying them.

## Running the Application
Execute `go run cmd/main.go` to launch the CLI. Optionally pass a stream type (`logs`, `metrics`, `traces`) or `--endpoint` to specify the websocket server.

