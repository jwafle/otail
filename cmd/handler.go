package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/jwafle/otail/internal/telemetry"
	"github.com/jwafle/otail/internal/view/message"
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
	return render(c, http.StatusOK, page.Logs())
}

func (app *application) metrics(c echo.Context) error {
	return render(c, http.StatusOK, page.Metrics())
}

func (app *application) traces(c echo.Context) error {
	return render(c, http.StatusOK, page.Traces())
}

type Event struct {
	ID      int
	Data    []byte
	Event   []byte
	Retry   []byte
	Comment []byte
}

func (e *Event) MarshalTo(w io.Writer) error {
	if len(e.Data) == 0 && len(e.Comment) == 0 {
		return nil
	}

	if len(e.Data) > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", e.ID); err != nil {
			return err
		}
		sd := bytes.Split(e.Data, []byte("\n"))
		for i := range sd {
			if _, err := fmt.Fprintf(w, "data: %s\n", sd[i]); err != nil {
				return err
			}
		}
		if len(e.Event) > 0 {
			if _, err := fmt.Fprintf(w, "event: %s\n", e.Event); err != nil {
				return err
			}
		}
		if len(e.Retry) > 0 {
			if _, err := fmt.Fprintf(w, "retry: %s\n", e.Retry); err != nil {
				return err
			}
		}
	}

	if len(e.Comment) > 0 {
		if _, err := fmt.Fprintf(w, "comment: %s\n", e.Comment); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "\n"); err != nil {
		return err
	}

	return nil
}

func (app *application) serveTelemetryStream(tk telemetry.Kind) echo.HandlerFunc {
	return func(c echo.Context) error {
		w := c.Response()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		w.WriteHeader(http.StatusOK)
		w.Flush()

		i := 0

		for {
			select {
			case <-c.Request().Context().Done():
				log.Printf("SSE client disconnected, ip: %v", c.RealIP())
				return nil
			case rawMessage := <-app.stream.Messages():
				parsedMessage := telemetry.Parse(rawMessage)
				if parsedMessage.Kind == tk {
					// Render the templ component into HTML
					var buf bytes.Buffer
					tmpl := message.Message(strconv.Itoa(i), string(parsedMessage.IndentedLines))
					if err := tmpl.Render(c.Request().Context(), &buf); err != nil {
						return echo.NewHTTPError(http.StatusInternalServerError, "render failed: "+err.Error())
					}

					// Now marshal that HTML as the SSE data
					messageEvent := &Event{
						ID:    i,
						Data:  buf.Bytes(),
						Event: []byte(parsedMessage.Kind.String()),
					}
					if err := messageEvent.MarshalTo(w); err != nil {
						return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
					}
					w.Flush()
					i++
				} else {
					continue
				}
			case err := <-app.stream.Errors():
				return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
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
