# `gpp/http` WebSocket Handler Specification

## Status

This specification defines the initial, deliberately small WebSocket API in
`gpp/http`. It exposes `golang.org/x/net/websocket.Conn` directly and does not
add a WebSocket class hierarchy or a separate connection lifecycle framework.

## Goals

- Mark WebSocket endpoints with `http.WebSocket(path)` on a handler method.
- Keep ordinary HTTP handler signatures and server or mountable route classes.
- Give each connection handler its own invocation and local state.
- Delegate the WebSocket handshake and framing to `golang.org/x/net/websocket`.
- Keep the initial Go++ connection API small.

## Non-goals

The initial API does not provide:

- a shared client registry, room abstraction, or broadcast service;
- `OnConnect`, `OnMessage`, or `OnDisconnect` override hooks;
- automatic JSON message dispatch, heartbeat policy, or reconnect behavior;
- a client-side WebSocket dialer;

Applications may build shared connection registries when they need broadcasts.

## Route declaration

`WebSocket(path string)` is a method annotation:

```gpp
annotation WebSocket(path string) on method
```

The annotated method must use the ordinary HTTP handler signature:

```gpp
func Chat(ctx *http.Context) error @{http.WebSocket("/chat")}
```

The route is registered as a `GET` endpoint. It can be declared on a class
derived from `http.Server` or on a class marked `http.Mountable`. Mounted
classes use the same prefix and mount-address rules as ordinary HTTP routes.
Registering an HTTP `GET` route at the same effective path is a route conflict.

The annotation only selects the transport and route. The handler owns its
message loop, connection-local state, and application-specific cleanup.

## Handler and connection API

`http.Context` exposes the upgraded connection directly through `Conn`:

```gpp
class Context {
    Conn *websocket.Conn
}
```

`Conn` is non-nil only while an annotated WebSocket handler is running and is
the actual `*websocket.Conn` passed to the handler by `golang.org/x/net/websocket`.
The `gpp/http` package adds `Read(&message)` and `Write(message)` extensions for
strings. These call `websocket.Message.Receive` and `websocket.Message.Send`, so
they receive and send whole text messages. The connection's native
`Read([]byte)` and `Write([]byte)` methods remain available with their
`io.Reader`/`io.Writer` behavior and `(n, error)` results. Use
`websocket.Message.Receive`/`Send` directly when you need whole binary messages.
`Conn.Close()` closes the underlying connection.

The framework closes the connection after the handler returns. Handlers may
also call `Close` explicitly. HTTP response helpers such as `Text` and `JSON`
must not be used after the connection has been upgraded.

## Connection-local and shared state

Each successful connection invokes the handler method independently. Local
variables in that invocation are connection-local and remain available for the
duration of the handler's read loop.

The mounted server or route-class instance may serve multiple connections
concurrently. Mutable fields on that instance are shared application state and
must be synchronized by the application. A handler may store the connection in
a shared hub when other handlers or background work need to send messages to
it. No shared hub is created automatically.

## Request and server hooks

`BeforeRequest` runs before the WebSocket handshake. If it returns an error,
the handshake is not attempted and the regular HTTP error handler is used.
`AfterRequest` runs after the WebSocket handler returns. The request lifecycle
therefore covers the full duration of the connection handler.

Once the upgrade succeeds, handler errors cannot be returned as HTTP responses.
The server logs the error and closes the WebSocket connection. Errors from
`AfterRequest` are handled the same way. A failed handshake does not invoke the
WebSocket handler.

The initial implementation does not maintain a registry of upgraded
connections. `Server.Shutdown` therefore does not proactively close or wait for
active WebSocket handlers. Applications that need coordinated shutdown may
track their sockets in application-owned shared state.

## Protocol and dependency behavior

The server implementation delegates WebSocket upgrade and protocol behavior to
`golang.org/x/net/websocket`. The framework must not implement WebSocket frame
parsing or handshake logic itself. Origin validation and other handshake
behavior follow that package's server handler behavior.

The generated Go module needs the `golang.org/x/net` module when an application
uses this API. `gpp build` and `gpp run` resolve generated imports through the
normal Go module workflow.

## Example

```gpp
import "fmt"
import "gpp/http"

class App : http.Server {
    func Echo(ctx *http.Context) error @{http.WebSocket("/echo")} {
        received := 0
        for {
            var message string
            if err := ctx.Conn.Read(&message); err != nil {
                return nil
            }

            received++
            reply := fmt.Sprintf("message %d: %s", received, message)
            if err := ctx.Conn.Write(reply); err != nil {
                return err
            }
        }
    }
}
```

Each connection has its own `received` counter. The handler does not share
socket state with other clients.
