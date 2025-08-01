package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jwafle/otail/internal/transport"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
)

type application struct {
	stream *transport.Stream
}

func main() {
	esrv := echo.New()
	esrv.Logger.SetLevel(log.DEBUG)
	port := flag.String("port", "4000", "port for ui")
	endpoint := flag.String("endpoint", "ws://localhost:12001", "endpoint for websocket connection")
	flag.Parse()

	// Setup graceful shutdown context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Initialize stream
	stream, err := transport.Dial(ctx, *endpoint, "http://localhost", &transport.Config{})
	if err != nil {
		esrv.Logger.Fatal("failed to connect to websocket:", err)
	}
	defer stream.Close()

	app := &application{
		stream: stream,
	}

	//--------------------------
	//middleware provided by echo
	//--------------------------
	//panic recover
	esrv.Use(middleware.Recover())
	//body limit
	esrv.Use(middleware.BodyLimit("35K"))
	//secure header
	esrv.Use(middleware.Secure())
	//timeout
	esrv.Use(middleware.TimeoutWithConfig(middleware.TimeoutConfig{
		Timeout: 5 * time.Second,
	}))
	//logger
	esrv.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: `{"time":"${time_rfc3339_nano}","remote_ip":"${remote_ip}",` +
			`"host":"${host}","method":"${method}","uri":"${uri}",` +
			`"status":${status},"error":"${error}","latency_human":"${latency_human}"` +
			`` + "\n",
		CustomTimeFormat: "2006-01-02 15:04:05.00000",
	}))

	//routes
	esrv.GET("/", app.home)
	esrv.GET("/metrics", app.metrics)
	esrv.GET("/logs", app.logs)
	esrv.GET("/traces", app.traces)
	esrv.GET("/metrics/sse", app.streamMetrics)
	esrv.GET("/logs/sse", app.streamLogs)
	esrv.GET("/traces/sse", app.streamTraces)

	// Start server in a goroutine
	go func() {
		if err := esrv.Start(":" + *port); err != nil && err != http.ErrServerClosed {
			esrv.Logger.Fatal("shutting down server")
		}
	}()

	// Wait for interrupt signal
	<-ctx.Done()

	// Gracefully shutdown with a timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := esrv.Shutdown(shutdownCtx); err != nil {
		esrv.Logger.Fatal(err)
	}
}
