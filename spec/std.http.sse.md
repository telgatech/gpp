# `gpp/http` Server-Sent Events Specification

## Status

This specification defines the initial Server-Sent Events (SSE) support in
`gpp/http`. It provides an event writer on every HTTP request context and an
optional marker annotation for readability.

## Goals

- Write correctly framed Server-Sent Events through the current HTTP response.
- Let handlers use normal `http.GET(path)` routing and request lifecycle hooks.
- Keep SSE event data as strings while correctly framing multiline data.
- Flush each event so clients can receive it while the handler is still running.
- Keep the API small and follow the WHATWG event-stream format.

## Route declaration

SSE uses an ordinary GET route. `http.SSE` is an optional marker annotation that
labels the handler as an event stream for readers; it does not register a route,
change dispatch, or enable streaming by itself. A handler can omit it and still
write events through `ctx.SSE`.

```gpp
annotation SSE on method
```

```gpp
func Events(ctx *http.Context) error @{
    http.GET("/events"),
    http.SSE
} {
    return ctx.SSE.Write("ready")
}
```

SSE is normally served over GET because browsers use the `EventSource` API,
which reconnects to a GET endpoint. Applications that need to authenticate,
filter, or initialize a stream can use ordinary query parameters, cookies,
headers, and request hooks. The annotation does not constrain the method
signature or handler behavior.

## Event writer

Every `http.Context` has an `SSE` writer. Its method is:

```gpp
func Write(
    data string,
    event string = "",
    id *string = nil,
    retryMS *int = nil,
) error
```

`data` is required. `event` is omitted when empty. A nil `id` or `retryMS`
omits that field; a non-nil pointer includes it, including an empty ID or a
zero retry delay. Negative retry delays are rejected. Event names may not
contain line breaks. IDs may not contain NUL or line breaks, as required by the
event-stream format. Invalid UTF-8 is rejected.

Each call emits one complete event block and flushes it:

```text
event: update
id: 42
retry: 3000
data: first line
data: second line

```

Carriage returns and CRLF pairs in `data` are normalized to line feeds, then
each resulting line is prefixed with `data: `. The final blank line terminates
the event. An empty data string is encoded as an empty `data:` field.

## Response behavior

The first successful `Write` sets `Content-Type` to
`text/event-stream; charset=utf-8`, sets `Cache-Control: no-cache` if the
handler has not already set it, commits status 200, writes the event, and
flushes the response. Later calls append and flush more events. The response
writer must support flushing; a flush error is returned to the handler.

After the first event commits the response, ordinary response helpers cannot
replace it with a new status or body. A handler error after the stream begins
is logged because an HTTP error response can no longer be sent. The handler
owns the stream lifetime and should return when it is done or when a write
fails. `gpp/http` does not add a broker, heartbeat loop, client registry,
reconnect policy, or event persistence.

The implementation uses Go's `net/http.ResponseController` to flush and leaves
transport details such as proxy buffering, TLS, and client reconnection to the
HTTP deployment and EventSource client.

## Example

```gpp
import "time"
import http "gpp/http"

class App : http.Server {
    func Events(ctx *http.Context) error @{
        http.GET("/events"),
        http.SSE
    } {
        retryMS := 3000
        counter := 0
        for {
            counter++
            id := "{{counter}}"
            try {
                ctx.SSE.Write("update {{counter}}\nstream is active", "update", &id, &retryMS)
            } catch {
                return nil // the client disconnected
            }
            time.Sleep(time.Second)
        }
    }
}
```
