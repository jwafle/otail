# otail

otail is a command-line tool for streaming telemetry from the OpenTelemetry Collector's [remotetapprocessor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/remotetapprocessor). It allows you to view telemetry data in real-time, making it easier to debug collector configurations and monitor telemetry flows.

## Architecture

otail is built using the [bubbletea]() TUI library. It provides a terminal-based user interface that displays telemetry data in a structured format, allowing users to filter and search through the data efficiently.

The tool connects to the OpenTelemetry Collector's remotetapprocessor and streams telemetry data directly to the terminal. This is achieved by creating a websocket client that connects to the OTEL collector remotetapprocessor which forwards telemetry data overwebsockets in [JSON protobuf encoding format](https://opentelemetry.io/docs/specs/otlp/#json-protobuf-encoding). It supports various data formats, including traces, metrics, and logs.

## Running Otail

Compile or run the application and specify which stream you want to view:

```bash
# defaults to logs
go run cmd/main.go

# explicitly select a stream
go run cmd/main.go logs
go run cmd/main.go metrics
go run cmd/main.go traces
```

Once running, a help bar at the bottom of the screen lists the key bindings. Use
**l**, **m**, or **t** to switch streams or **q** to quit.
