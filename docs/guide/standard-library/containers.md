# Containers

`gpp/container` provides generic, statically typed data structures for common
collection patterns. Import it explicitly:

```gpp
import "gpp/container"
```

Each structure stores its element type directly. For example,
`container.Queue[string]` accepts strings and returns strings; retrieving an
element does not require a type assertion.

## Choose a container

| Type | Use it for | Order |
| --- | --- | --- |
| `Set[T comparable]` | Unique values and membership checks | `Values()` order is unspecified |
| `Stack[T]` | Last-in, first-out work | Most recently pushed item comes out first |
| `Queue[T]` | First-in, first-out work | Earliest enqueued item comes out first |
| `Deque[T]` | Adding or removing at either end | Preserves front-to-back order |
| `List[T]` | Stable endpoint operations with linked storage | Front-to-back order |
| `PriorityQueue[T]` | Repeatedly selecting the next item by priority | Comparator decides which item comes out first |

The zero values of `Set`, `Stack`, `Queue`, `Deque`, and `List` are ready to
use. A `PriorityQueue` needs an ordering function and must be created with
`NewPriorityQueue`.

## Set

`Set[T comparable]` uses Go's built-in map storage. `Add` and `Remove` report
whether they changed the set:

```gpp
var seen container.Set[string]

if seen.Add("request-42") {
    println("first time seen")
}

if seen.Contains("request-42") {
    println("already recorded")
}

values := seen.Values()
```

`Values` returns a new slice. Its ordering is unspecified.

## Stack and queue

Use a stack for depth-first work and a queue for first-in, first-out work.
Empty pops and peeks return the zero value and `false`:

```gpp
var tasks container.Queue[string]
tasks.Enqueue("compile")
tasks.Enqueue("package")

next, ok := tasks.Dequeue() // "compile", true

var undo container.Stack[string]
undo.Push("edit file")
action, ok := undo.Pop() // "edit file", true
```

Both types expose `Len`, `IsEmpty`, and `Clear`.

## Deque

A deque supports operations at both ends. Push and pop operations are
amortized O(1):

```gpp
var recent container.Deque[string]
recent.PushFront("current")
recent.PushBack("older")

newest, hasNewest := recent.PopFront()
oldest, hasOldest := recent.PopBack()
```

`Front` and `Back` inspect either endpoint without removing it. An empty deque
returns `(zero, false)` from `PopFront`, `PopBack`, `Front`, and `Back`.

## List

`List[T]` is a typed doubly linked list. Endpoint insertions and removals are
O(1). It does not expose node handles, which keeps its internal links private:

```gpp
var history container.List[string]
history.PushBack("created")
history.PushBack("validated")
history.PushFront("received")

steps := history.Values() // ["received", "created", "validated"]
last, ok := history.PopBack()
```

`Values` returns a new front-to-back slice. `Front`, `Back`, `PopFront`, and
`PopBack` return `(zero, false)` when the list is empty.

## Priority queue

Provide a comparator that returns `true` when its first argument has higher
priority. A less-than comparator creates a min-first queue; a greater-than
comparator creates a max-first queue:

```gpp
queue := container.NewPriorityQueue[int](func(left, right int) bool {
    return left < right
})

queue.Push(8)
queue.Push(3)
queue.Push(5)

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
