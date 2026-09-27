# Suite-based tests

Go++ testing adds discoverable suites, shared setup, tags, priorities, and
assertion helpers while running on Go's testing infrastructure. Existing Go
tests can remain alongside Go++ suites.

## Compare a test function with a suite

A Go test function is a good fit for one focused case. A suite groups related
checks and common setup:

::: code-group

```go [Go test]
func TestUserName(t *testing.T) {
    user := User{Name: "Ada"}
    if user.Name != "Ada" {
        t.Fatalf("got %q", user.Name)
    }
}
```

```go [Go++ suite]
class UserTest : test.Suite {
    func NameIsPreserved() {
        user := User(Name: "Ada")
        Equal("Ada", user.Name)
    }
}
```

:::

## Organize and select suites

Tags and priorities make larger suites easier to run selectively:

```go
class UserTest : test.Suite @{test.Tag("crud")} {
    func CreatesUser() {
        user := User(Name: "Ada")
        Equal("Ada", user.Name)
    }
}
```

```sh
gpp test --tag crud .
```

Suites can share setup and assertion helpers, while Go's test runner still
reports failures and controls execution. See the [testing specification](/reference/specifications/testing).
