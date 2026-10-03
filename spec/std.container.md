# `gpp/container` Specification

## Status

This specification defines the initial generic container package. The first
implementation is bundled in `compiler/stdlib/gpp/container`.

## Goals

`gpp/container` provides a small set of typed data structures for Go++ programs.
Its containers use their element types directly in storage and public APIs.
They must not store elements as `any` or require callers to recover element
types with assertions.

The package intentionally provides data structures, not replacements for Go's
generic slice and map algorithms. Users should continue to use Go's `slices`
and `maps` packages for those operations.

## Package path

Import the package as:

```gpp
import "gpp/container"
```

The compiler distributes the package with the Go++ standard packages and
recognizes it as an official package.

The package source is ordinary Go++ and lowers to ordinary Go generic
declarations. It must be loadable by the compiler, documentation tooling, and
language tooling through the same official-package mechanism used by the
other bundled packages.

## Implementation requirements

1. Each container stores values using its declared generic type `T`.
2. Public methods accept and return `T` (or a typed result containing `T`).
3. Implementations must not delegate storage to `container/list`,
   `container/ring`, or `container/heap`, because those APIs store or exchange
   elements through `any`.
4. Implementations may use Go's typed built-in slices and maps. They must not
   reimplement slice or map utility algorithms that are already available in
   Go's `slices` and `maps` packages.
5. No container is concurrency-safe. Callers must synchronize shared access.
6. Zero values should be usable unless a type's behavior requires configuration.
7. A zero-value read or removal from an empty container must return the zero
   value of `T` and `false`; it must not panic.

The package may implement familiar container algorithms itself where Go does
not expose a fully typed generic implementation. That is the tradeoff for
keeping element storage and operations statically typed end to end.

## Initial API

The first release includes `Set[T comparable]`, `Stack[T]`, `Queue[T]`,
`Deque[T]`, `List[T]`, and `PriorityQueue[T]`. A circular ring, ordered map,
ordered set, multimap, and linked-list node handles are outside this initial
scope.

### Set

`Set[T comparable]` is a map-backed set. Its zero value is ready to use.

```gpp
set := container.Set[string]{}
inserted := set.Add("ada")
exists := set.Contains("ada")
removed := set.Remove("ada")
```

Required methods:

```gpp
func (this *Set[T]) Add(value T) bool
func (this *Set[T]) Remove(value T) bool
func (this *Set[T]) Contains(value T) bool
func (this *Set[T]) Len() int
func (this *Set[T]) IsEmpty() bool
func (this *Set[T]) Clear()
func (this *Set[T]) Values() []T
```

`Add` returns true only when it inserts a previously absent value. `Remove`
returns true only when it removes a present value. `Values` returns a new slice;
its order is unspecified. Mutating the returned slice must not modify the set.

### Stack

`Stack[T]` is a last-in, first-out container. Its zero value is ready to use.

```gpp
stack := container.Stack[int]{}
stack.Push(10)
value, ok := stack.Pop()
```

Required methods:

```gpp
func (this *Stack[T]) Push(value T)
func (this *Stack[T]) Pop() (T, bool)
func (this *Stack[T]) Peek() (T, bool)
func (this *Stack[T]) Len() int
func (this *Stack[T]) IsEmpty() bool
func (this *Stack[T]) Clear()
```

`Pop` removes and returns the most recently pushed value. `Peek` returns that
value without removing it. On an empty stack, both return `(zero, false)`.

### Queue

`Queue[T]` is a first-in, first-out container. Its zero value is ready to use.

```gpp
queue := container.Queue[string]{}
queue.Enqueue("first")
value, ok := queue.Dequeue()
```

Required methods:

```gpp
func (this *Queue[T]) Enqueue(value T)
func (this *Queue[T]) Dequeue() (T, bool)
func (this *Queue[T]) Peek() (T, bool)
func (this *Queue[T]) Len() int
func (this *Queue[T]) IsEmpty() bool
func (this *Queue[T]) Clear()
```

`Dequeue` removes and returns the earliest enqueued value. `Peek` returns the
next value without removing it. On an empty queue, both return `(zero, false)`.
The implementation should avoid shifting every remaining element on each
dequeue; it may compact its typed slice when appropriate.

### Deque

`Deque[T]` is a double-ended queue. Its zero value is ready to use.

```gpp
deque := container.Deque[int]{}
deque.PushFront(1)
deque.PushBack(2)
first, ok := deque.PopFront()
last, ok := deque.PopBack()
```

Required methods:

```gpp
func (this *Deque[T]) PushFront(value T)
func (this *Deque[T]) PushBack(value T)
func (this *Deque[T]) PopFront() (T, bool)
func (this *Deque[T]) PopBack() (T, bool)
func (this *Deque[T]) Front() (T, bool)
func (this *Deque[T]) Back() (T, bool)
func (this *Deque[T]) Len() int
func (this *Deque[T]) IsEmpty() bool
func (this *Deque[T]) Clear()
```

Push and pop operations must preserve the logical order of remaining values.
On an empty deque, `PopFront`, `PopBack`, `Front`, and `Back` return
`(zero, false)`. Endpoint operations should be amortized O(1); implementations
may use a growable circular buffer backed by `[]T`.

### List

`List[T]` is a doubly linked list with typed nodes and typed values. Its zero
value is ready to use. Node handles are not part of the initial public API.

```gpp
list := container.List[string]{}
list.PushBack("first")
list.PushFront("before")
value, ok := list.PopFront()
```

Required methods:

```gpp
func (this *List[T]) PushFront(value T)
func (this *List[T]) PushBack(value T)
func (this *List[T]) PopFront() (T, bool)
func (this *List[T]) PopBack() (T, bool)
func (this *List[T]) Front() (T, bool)
func (this *List[T]) Back() (T, bool)
func (this *List[T]) Len() int
func (this *List[T]) IsEmpty() bool
func (this *List[T]) Clear()
func (this *List[T]) Values() []T
```

Endpoint insertion and removal must be O(1). `Values` returns a new slice in
front-to-back order. The list does not expose internal nodes or links.

### Priority queue

`PriorityQueue[T]` is a typed binary heap. Since arbitrary `T` values have no
universal ordering, construction requires a comparator. The zero value is not
usable; callers must create the queue with `NewPriorityQueue`.

```gpp
queue := container.NewPriorityQueue[int](func(a, b int) bool {
    return a < b
})
queue.Push(7)
queue.Push(2)
smallest, ok := queue.Pop()
```

Required constructor and methods:

```gpp
func NewPriorityQueue[T](less func(T, T) bool) *PriorityQueue[T]
func (this *PriorityQueue[T]) Push(value T)
func (this *PriorityQueue[T]) Pop() (T, bool)
func (this *PriorityQueue[T]) Peek() (T, bool)
func (this *PriorityQueue[T]) Len() int
func (this *PriorityQueue[T]) IsEmpty() bool
func (this *PriorityQueue[T]) Clear()
```

`less(a, b)` must return true when `a` has higher priority than `b`. With
`a < b`, the smallest item is returned first; with `a > b`, the largest item
is returned first. The comparator must define a consistent strict ordering.
`Pop` removes the highest-priority value; `Peek` leaves it in the queue. On an
empty queue, both return `(zero, false)`. Passing a nil comparator is invalid
and must panic during construction with a clear message.

## Common behavior

- Container methods do not return errors for ordinary empty-container
  operations; they use the `(T, bool)` convention above.
- `Clear` removes all stored elements and leaves the container ready for reuse.
  A priority queue retains its comparator.
- Containers own their storage. Slices returned by `Values` are independent
  copies of the container's element sequence.
- Elements themselves are copied according to Go assignment semantics. A
  container does not deep-copy referenced data inside `T`.
- Iteration order is insertion/order-of-structure for `Stack`, `Queue`,
  `Deque`, and `List` as described by each API; `Set.Values` has unspecified
  order; `PriorityQueue` does not promise a sorted `Values` or iteration view.

## Validation requirements

The implementation must include focused tests for:

- zero-value operations;
- empty pops and peeks;
- preservation of element order;
- repeated growth and reuse after `Clear`;
- duplicate set insertions and missing removals;
- deque wraparound and growth;
- list transitions between empty, one-item, and multiple-item states; and
- priority ordering for both min-first and max-first comparators.

Compile-time examples must demonstrate that values of the wrong element type
are rejected by each typed API. No API may expose an `any`-typed element or
require a runtime type assertion to retrieve a stored value.
