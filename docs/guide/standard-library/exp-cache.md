# `gpp/exp/cache`

`gpp/exp/cache` provides typed values with TTL expiration. `cache.Cache()` uses a
concurrency-safe in-process store with a 1 GiB payload budget; values are
encoded with GOB when written and decoded to the type requested by `Get[T]`.
The store evicts least-recently-used entries when it reaches its memory or
optional entry-count limit, and prunes expired entries automatically.

```gpp
import (
    "fmt"
    "time"
    "gpp/encoding"
    cache "gpp/exp/cache"
)

class Report @{encoding.Serializable} {
    Name string
    Total int
}

reports := cache.Cache()
report := reports.GetOrSet[*Report]("report:weekly", 5*time.Minute, func() (*Report, error) {
    fmt.Println("building report")
    return Report(Name: "weekly", Total: 42), nil
})
fmt.Println(report.Name, report.Total)

bounded := cache.Cache(maxMemory: 256*1024*1024, maxEntries: 10000)
```

`Get[T](key)` and `GetOrSet[T](key, ttl, load)` have Go signatures that return
`(value, error)`. In GPP, assigning either call to one value raises a non-nil
error as an exception; use `try`/`catch` to handle it locally, or assign both
results to handle the error explicitly. A missing or expired key returns
`cache.ErrNotFound`. `GetOrSet` calls `load` only when the key is missing, then
caches its successful result for the given TTL. Loader errors are not cached.
Concurrent misses may call the loader more than once.

`Set(key, value)` stores a value without expiration. `SetWithTTL(key, value,
ttl)` sets an expiration; zero means no expiration. `Delete(pattern)` removes
every key matching the regular expression, and `Clear()` removes every entry
from the store.

`cache.Cache()` defaults to a 1 GiB memory budget and no entry-count limit. Pass
`maxMemory` in bytes and/or `maxEntries` to set limits. The memory budget
counts key bytes plus encoded value bytes; it is a hard cap on retained cache
payload, excluding map/list bookkeeping and Go runtime allocation overhead. A
single entry larger than the budget returns `cache.ErrEntryTooLarge`. A zero
memory limit accepts no non-empty entry; a nonpositive entry limit disables
the count limit. When adding an entry would cross a limit, least-recently-used
entries are evicted first. Reads update recency.

Expired entries are pruned automatically at their nearest expiration and
before writes. `Get` also checks expiration. The in-process store is not
durable.

Values must be GOB-encodable. GPP classes that need generated serialization,
including classes with private fields, should use `@encoding.Serializable`.
External backends can implement `cache.Store` and be supplied to
`cache.Cache(store: backend)`. A store should be dedicated to this cache because
`Clear()` removes all of its entries; its `Delete` method receives a regular
expression pattern.

The package is experimental; its API may change.
