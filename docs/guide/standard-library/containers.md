# Containers

`gpp/container` provides generic, statically typed data structures for common
collection patterns. Import it explicitly:

```gpp
import (
	"fmt"
	"gpp/container"
)
```

Each structure stores its element type directly. For example,
`container.Queue[string]` accepts strings and returns strings; retrieving an
element does not require a type assertion.

## Choose a container

| Type | Use it for | Order |
| --- | --- | --- |
| `Set[T comparable]` | Unique values and membership checks | `Values()` order is unspecified |
| `OrderedMap[K, V]` | Key/value lookup with predictable traversal | Insertion order |
| `Stack[T]` | Last-in, first-out work | Most recently pushed item comes out first |
| `Queue[T]` | First-in, first-out work | Earliest enqueued item comes out first |
| `Deque[T]` | Adding or removing at either end | Preserves front-to-back order |
| `List[T]` | Stable endpoint operations with linked storage | Front-to-back order |
| `Ring[T]` | Cyclic traversal and round-robin work | Wraps from either end |
| `PriorityQueue[T]` | Repeatedly selecting the next item by priority | Comparator decides which item comes out first |

The zero values of `Set`, `OrderedMap`, `Stack`, `Queue`, `Deque`, `List`, and
`Ring` are ready to use. A `PriorityQueue` needs an ordering function, so pass
one when constructing it with `PriorityQueue[T](comparator)`.

## Set

`Set[T comparable]` uses Go's built-in map storage. `Add` and `Remove` report
whether they changed the set. `Add` accepts any number of values and returns
true if at least one new value was added. Duplicate values are ignored.
`Clear` empties the set, and the set can be reused afterward:

```gpp
seen := container.Set[string]("request-42", "request-43", "request-42")
seen.Add("request-44", "request-45")

if seen.Add("request-46") {
    println("first time seen")
}

if seen.Contains("request-42") {
    println("already recorded")
}

values := seen.Values()
seen.Clear()
```

`Values` returns a new slice. Its ordering is unspecified.

Set operations return new sets and leave both inputs unchanged:

```gpp
var allowed container.Set[string]
allowed.Add("read")
allowed.Add("write")

var requested container.Set[string]
requested.Add("write")
requested.Add("admin")

all := allowed.Union(requested)        // read, write, admin
shared := allowed.Intersect(requested) // write
missing := requested.Minus(allowed)    // admin
```

## Ordered map

`OrderedMap[K, V]` provides map lookup while keeping keys in insertion order.
Updating an existing key keeps its position; deleting and adding it again
moves it to the end. The zero value is ready to use:

```gpp
var settings container.OrderedMap[string, int]
settings.Set("retries", 3)
settings.Set("timeout", 10)
settings.Set("retries", 5)

value, found := settings.Get("retries") // 5, true
fmt.Println(settings.Keys())            // [retries timeout]

entries := settings.Entries()

for _, entry := range entries {
    fmt.Printf("%s=%d ", entry.Key, entry.Value)
}
```

`Get` returns `(zero, false)` for a missing key. `Keys` and `Values` return
new slices in insertion order. `Entries` returns a snapshot slice of
`OrderedMapEntry` values with public `Key` and `Value` fields. Iterate over the
snapshot with GPP's `for range` syntax.

## Stack and queue

Use a stack for depth-first work and a queue for first-in, first-out work.
Pass initial values in insertion order when constructing either type. Empty
pops and peeks return the zero value and `false`:

```gpp
tasks := container.Queue[string]("compile", "package")

next, ok := tasks.Dequeue() // "compile", true

undo := container.Stack[string]("edit file")
action, ok := undo.Pop() // "edit file", true
```

Both types expose `Len`, `IsEmpty`, and `Clear`.

## Deque

A deque supports operations at both ends. Push and pop operations are
amortized O(1):

```gpp
recent := container.Deque[string]("current", "older")

newest, hasNewest := recent.PopFront()
oldest, hasOldest := recent.PopBack()
```

`Front` and `Back` inspect either endpoint without removing it. An empty deque
returns `(zero, false)` from `PopFront`, `PopBack`, `Front`, and `Back`.

## List

`List[T]` is a typed doubly linked list. Endpoint insertions and removals are
O(1). It does not expose node handles, which keeps its internal links private:

```gpp
history := container.List[string]("created", "validated")
history.PushFront("received")

steps := history.Values() // ["received", "created", "validated"]
last, ok := history.PopBack()
```

`Values` returns a new front-to-back slice. `Front`, `Back`, `PopFront`, and
`PopBack` return `(zero, false)` when the list is empty.

## Ring

`Ring[T]` stores a circular doubly linked sequence. It is useful when work
should cycle through a set of values, such as a round-robin schedule. The zero
value is ready to use. The first inserted value becomes current:

```gpp
schedule := container.Ring[string]("compile", "test", "package")

current, hasCurrent := schedule.Current() // "compile", true
next, hasNext := schedule.Next()          // "test", true
previous, hasPrevious := schedule.Prev()  // "compile", true
schedule.Move(-1)                          // wraps to "package"
```

`Next` and `Prev` move the current position and wrap around the ring. `Move`
accepts positive or negative steps. `RemoveCurrent` removes the current value
and makes its successor current. `Values` returns the sequence from its first
value to its last, independent of the current position.

## Priority queue

Provide a comparator that returns `true` when its first argument has higher
priority. A less-than comparator creates a min-first queue; a greater-than
comparator creates a max-first queue:

```gpp
queue := container.PriorityQueue[int](func(left, right int) bool {
    return left < right
}, 8, 3, 5)

next, ok := queue.Pop() // 3, true
```

The comparator must define a consistent strict ordering and must not be nil.
Passing nil panics during construction. Empty `Peek` and `Pop` return
`(zero, false)`.

## Shared behavior

All containers are single-goroutine by default; protect shared access with
ordinary Go synchronization. `Clear` removes stored elements. Container
operations do not return errors for empty reads or removals. Values are copied
using normal Go assignment semantics, so containers do not deep-copy pointers,
slices, maps, or other referenced values inside an element.

The runnable example in [`examples/containers.gpp`](../../../examples/containers.gpp)
shows each type in use.
