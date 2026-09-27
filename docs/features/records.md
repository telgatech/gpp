# Structural records

Records represent small data shapes without declaring a named class first.
They are handy for local results, configuration fragments, and values passed
between nearby functions when a reusable nominal type would add ceremony.

## Name fields at the point of use

Go usually introduces an anonymous shape with an anonymous struct type. A
Go++ record gives fields names directly in the value expression:

::: code-group

```go [Go anonymous struct]
user := struct {
    Name   string
    Active bool
}{Name: "Ada", Active: true}
```

```go [Go++ record]
user := record(
    Name: "Ada",
    Active: true,
)
```

:::

The compiler derives a deterministic Go struct shape from the record fields.
Field access remains statically checked:

```go
if user.Active {
    fmt.Println(user.Name)
}
```

## Nest records for related data

Records can contain other records and work naturally with inferred local
values:

```go
response := record(
    ok: true,
    user: record(
        Name: "Ada",
        Profile: record(Role: "admin"),
    ),
)

fmt.Println(response.user.Profile.Role)
```

Use a class or named Go struct when a shape is part of a package's long-lived
public contract. See the [record specification](/reference/specifications/records)
for type identity and visibility rules.
