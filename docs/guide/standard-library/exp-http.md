# `gpp/exp/http`

This experimental package provides an HTTP client. The server API remains in [`gpp/http`](./http).

## HTTP client

`gpp/exp/http.Client` wraps Go's `net/http.Client` with a cookie jar and typed JSON
helpers. Each client has its own cookie jar by default, so cookies set by one
response are sent on later requests to the same site:

```gpp
import (
    "time"
    http "gpp/exp/http"
)

class User {
    Name string
}

client := http.Client(BaseURL: apiURL, Timeout: 10 * time.Second)
user := client.Get[User]("/users/1")
created := client.Post[User]("/users", newUser)
summary := client.Get[struct(name: string, level: int)]("/users/summary")
print(summary.name, summary.level)
```

Without a type argument, `Get` and `Post` return raw responses. With `[T]`,
they decode JSON into `T`. Request contexts are optional; omit the final
context argument to use `context.Background()`, or pass one to control
cancellation and deadlines:

```gpp
import (
    "context"
    "time"
)

ctx, cancel := context.WithTimeout(context.Background(), 3 * time.Second)
defer cancel()
user := client.Get[User]("/users/1", ctx)
```

`Get[T]`, `Post[T]`, and `DoJSON[T]` decode successful JSON responses into the
requested type, including anonymous struct types whose fields retain their
declared types. Anonymous struct request values are encoded with their field
names. A non-2xx response becomes an error that includes the
status and up to 4 KiB of its body. As with other GPP calls that return errors,
an unhandled error is raised automatically. `SetTimeout` accepts a
`time.Duration`; zero leaves the request without a total timeout. Set the
transport, timeout, and cookie jar before using the client concurrently. The
timeout can also be changed later with `SetTimeout`. `BaseURL` must be an
absolute HTTP or HTTPS URL without a query or fragment. Relative paths are
resolved under its path prefix, so `/users/1` with a base of
`https://api.example.com/v1` requests `/v1/users/1`. Absolute request URLs
bypass the base URL. Use `SetBaseURL` to change the base later.

For requests that need custom methods or headers, use `Do` with a normal
`net/http.Request`. `Get`, `Post`, `PostForm`, and `Upload` return the raw
`net/http.Response`; check its status and close `Body` when finished. Raw `Post`
accepts a content type and an `io.Reader` body:

```gpp
import (
    "strings"
    http "gpp/exp/http"
)

client := http.Client()
postResponse := client.Post(apiURL, "text/plain", strings.NewReader("hello"))
defer postResponse.Body.Close()

formResponse := client.PostForm(apiURL, struct(name: "Ada", level: 3))
defer formResponse.Body.Close()

uploadResponse := client.Upload(
    uploadURL,
    struct(description: "report"),
    map[string]string{"file": "./report.pdf"},
)
defer uploadResponse.Body.Close()
```

`PostForm` and `Upload` accept anonymous structs for form fields. Each field
name becomes its form key, and its value is formatted as text at the HTTP
boundary. Slice fields produce repeated form values. The file map passed to
`Upload` maps file field names to paths; each path's base name is used as the
uploaded filename. Use `url.Values` with `PostForm`, or `UploadFiles` with
`url.Values` and `[]*http.UploadFile`, when you need repeated values or custom
filenames. Both upload methods stream files from disk. The client does not
retry requests automatically; applications can decide whether a request is
safe to retry.
