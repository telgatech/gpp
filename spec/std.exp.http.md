# Experimental HTTP client

This specification describes the experimental `gpp/exp/http` package. The HTTP server and annotations remain in [`gpp/http`](std.http.md).

## `gpp/exp/http`

`Client` wraps `net/http.Client`. Each instance creates a cookie jar unless a
caller replaces it with `SetCookieJar`. `Get[T]`, `Post[T]`, and `DoJSON[T]`
decode successful JSON responses into the requested type and return errors for
non-2xx statuses or invalid JSON. Calls to `Get` and `Post` without `[T]`
return raw HTTP responses.
`Do`, `Get`, `Post`, `PostForm`, and `Upload` return ordinary
`net/http.Response` values; callers own and must close their response bodies.
`Post` accepts a content type and `io.Reader` body without requiring a response
type. Multipart uploads stream files from paths described by `UploadFile`
values. The client does not retry requests automatically.

Conceptual API:

```gpp
import (
    "context"
    "io"
    "net/url"
    nethttp "net/http"
    "time"
)

class Client {
    BaseURL string
    Timeout time.Duration
    func SetBaseURL(baseURL string) error
    func SetTimeout(timeout time.Duration)
    func SetTransport(transport nethttp.RoundTripper)
    func SetCookieJar(jar nethttp.CookieJar)
    func Do(request *nethttp.Request) (*nethttp.Response, error)
    func Get(url string, ctx context.Context = nil) (*nethttp.Response, error)
    func Get[T](url string, ctx context.Context = nil) (T, error)
    func Post(url string, contentType string, body io.Reader, ctx context.Context = nil) (*nethttp.Response, error)
    func Post[T](url string, value any, ctx context.Context = nil) (T, error)
    func DoJSON[T](request *nethttp.Request) (T, error)
    func PostForm(url string, values any, ctx context.Context = nil) (*nethttp.Response, error)
    func Upload(url string, values any, files map[string]string, ctx context.Context = nil) (*nethttp.Response, error)
    func UploadFiles(url string, values url.Values, files []*UploadFile, ctx context.Context = nil) (*nethttp.Response, error)
}

class UploadFile {
    Field string
    Path string
    Filename string
}
```

`Client(BaseURL: "https://api.example.com/v1", Timeout: 10 * time.Second)`
creates a client with its own cookie jar, a path-prefixed base URL, and a total
request timeout. Relative request paths resolve under the base path; absolute
URLs bypass it. `Client()` leaves the base URL empty and timeout at zero,
matching `net/http.Client`. `SetBaseURL`, `SetTimeout`, `SetTransport`, and
`SetCookieJar` configure the client; configure it before concurrent use.
The final context argument is optional on both `Get` and `Post` overloads, as
well as `PostForm`, `Upload`, and `UploadFiles`. Omitting it uses
`context.Background()`; pass a context to set cancellation or a deadline.
Explicit `[T]` selects the JSON decoding overload; omitting `[T]` selects the
raw response overload.
`PostForm` and `Upload` accept anonymous structs for form fields. Field names
become form keys and values are formatted using `fmt.Sprint` at the HTTP
boundary; slice fields produce repeated values. `Upload` also accepts file paths
keyed by upload field name. `url.Values` can be passed to `PostForm` or
`UploadFiles` for repeated form values, and `UploadFile` supports a custom
uploaded filename.

`Get[T]` and `Post[T]` also accept generated anonymous structs as their JSON
response type. Their fields are decoded by name while retaining each field's
declared Go++ type. Anonymous struct request values passed to `Post[T]` are
encoded with their field names and values.
