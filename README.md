# otail

otail is a command-line tool for streaming telemetry from the OpenTelemetry Collector's [remotetapprocessor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/remotetapprocessor). It allows you to view telemetry data in real-time, making it easier to debug collector configurations and monitor telemetry flows.

## Architecture

otail is built using the [bubbletea]() TUI library. It provides a terminal-based user interface that displays telemetry data in a structured format, allowing users to filter and search through the data efficiently.

The tool connects to the OpenTelemetry Collector's remotetapprocessor and streams telemetry data directly to the terminal. This is achieved by creating a websocket client that connects to the OTEL collector remotetapprocessor which forwards telemetry data overwebsockets in [JSON protobuf encoding format](https://opentelemetry.io/docs/specs/otlp/#json-protobuf-encoding). It supports various data formats, including traces, metrics, and logs.

## Running Otail

```bash
go run cmd/main.go --endpoint ws://127.0.0.1:12001
```

The endpoint defaults to `ws://127.0.0.1:12001`. You can also use `-e` as a
shorthand flag.
