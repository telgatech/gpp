# `gpp/test`

`gpp/test` provides Go++ test suites based on classes, with test methods discovered and run by the `gpp test` command. Assertions, setup hooks, tags, and priority annotations organize the checks without changing the language's class model.

## Why suites?

Go++ tests can use Go's `testing` package directly. A suite is useful when related checks share setup or need suite-level metadata, filtering, and a consistent assertion API. The suite remains a concrete class; individual methods describe test cases.

## Side by side: a focused test

A Go test function commonly looks like this, while a Go++ suite can put setup and related test cases together:

::: code-group

```go [Go++]
import "gpp/test"

class UserTest : test.Suite @{test.Tag("users")} {
	Name string

	func Setup() {
		this.Name = "Ada"
	}

	func KeepsTheName() {
		Equal("Ada", this.Name)
		True(this.Name != "")
	}
}
```

```go [Go]
func TestUserName(t *testing.T) {
	user := User{Name: "Ada"}
	if user.Name != "Ada" {
		t.Fatalf("got %q, want Ada", user.Name)
	}
}
```

:::

## More examples

Methods may carry their own tags or priority:

```go
func CreatesUser() @{test.Tag("crud"), test.Priority(test.High)} {
	user := NewUser("Ada")
	Equal("Ada", user.Name)
}

func RejectsEmptyName() @{test.Priority(test.Low)} {
	Equal("", User(Name: "").Name)
}
```

Run the suite or narrow execution by metadata:

```sh
gpp test examples/testing.gpp
gpp test --tag crud examples/testing.gpp
gpp test --priority high examples/testing.gpp
```

For tests that need Go testing features directly, a regular Go test function remains available. See the [testing specification](/reference/specifications/testing) and [testing feature guide](/features/testing).
