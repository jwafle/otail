package main

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/jwafle/otail/internal/telemetry"
	"github.com/jwafle/otail/internal/view/page"
	"github.com/labstack/echo/v4"
)

var exampleJSON = `
{
	"name":"Adrian Larion",
	"age":30,
	"city":"Bucharest"
}`

func (app *application) home(c echo.Context) error {
	return render(c, http.StatusOK, page.Home(exampleJSON))
}

func (app *application) logs(c echo.Context) error {
	return render(c, http.StatusOK, page.Home("Adrian Larion"))
}

func (app *application) metrics(c echo.Context) error {
	return render(c, http.StatusOK, page.Metrics())
}

func (app *application) traces(c echo.Context) error {
	return render(c, http.StatusOK, page.Home("Adrian Larion"))
}

type EchoHandlerFunc func(c echo.Context) error

func (app *application) serveTelemetryStream(tk telemetry.Kind) EchoHandlerFunc {
	return func(c echo.Context) error {
		w := c.Response()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		for {
			select {
			case <-c.Request().Context().Done():
				log.Printf("SSE client disconnected, ip: %v", c.RealIP())
				return nil
			case message := <-app.stream.Messages():
				parsedMessage := telemetry.Parse(message)
				if parsedMessage.Kind == tk {
					if _, err := w.Write(parsedMessage.IndentedLines); err != nil {
						return err
					}
					w.Flush()
				} else {
					continue
				}
			}
		}
	}
}

func (app *application) streamMetrics(c echo.Context) error {
	return app.serveTelemetryStream(telemetry.KindMetrics)(c)
}

func (app *application) streamLogs(c echo.Context) error {
	return app.serveTelemetryStream(telemetry.KindLogs)(c)
}

func (app *application) streamTraces(c echo.Context) error {
	return app.serveTelemetryStream(telemetry.KindTraces)(c)
}

// Helper function to capture templ component HTML as a string
func captureTemplHTML(component templ.Component) string {
	var sb strings.Builder
	err := component.Render(context.Background(), &sb)
	if err != nil {
		return ""
	}
	return sb.String()
}
