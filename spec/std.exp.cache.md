# Experimental Typed Cache

`gpp/exp/cache` provides typed GOB values over a replaceable byte store. The
default `cache.Cache()` uses a concurrency-safe in-process store with a 1 GiB
payload budget. It evicts least-recently-used entries when needed and prunes
expired entries automatically. The package is experimental and its API may
change.

## API

```gpp
class Cache {
    func init(maxMemory int = 1073741824, maxEntries int = 0)
    func init(store Store) error
    func Set[T any](key string, value T) error
    func SetWithTTL[T any](key string, value T, ttl time.Duration) error
    func Get[T any](key string) (T, error)
    func GetOrSet[T any](key string, ttl time.Duration, load func() (T, error)) (T, error)
    func Delete(pattern string) error
    func Clear() error
}

const DefaultMaxMemory int = 1073741824

class MemoryStore {
    func init(maxMemory int = 1073741824, maxEntries int = 0)
}

interface Store {
    Get(key string) ([]byte, bool, error)
    Set(key string, value []byte, ttl time.Duration) error
    Delete(pattern string) error
    Clear() error
}
```

`Set` writes without expiration. `SetWithTTL` accepts a positive duration or
zero for no expiration; negative durations are rejected. Empty keys are
rejected for writes and reads. `Get` returns `ErrNotFound` for a missing or
expired key. The methods have Go signatures with a trailing `error`; GPP's
implicit error promotion lets callers assign the value and raises the error as
an exception. Use `try`/`catch` for local handling or capture both results
explicitly. Encoded values use `gpp/encoding` GOB helpers.

`GetOrSet` returns a cached value when present. On `ErrNotFound`, it calls the
loader, stores a successful result with the supplied TTL, and returns it.
Loader and store errors are returned. Concurrent misses can invoke the loader
more than once; the cache does not provide single-flight coordination.

`Cache()` defaults to a 1 GiB memory budget and no entry-count limit. Set
`maxMemory` and/or `maxEntries` to choose smaller limits. The memory budget
counts each key's bytes plus its encoded value bytes; it is a hard limit on
that retained payload. Cache bookkeeping and Go runtime allocation overhead
are not included. A single entry larger than the budget is rejected with
`ErrEntryTooLarge`. A zero `maxMemory` accepts no non-empty entry; negative
values are treated as zero. A nonpositive `maxEntries` disables the entry-count
limit.

When a write would exceed either limit, the in-process store evicts least
recently used entries until the new entry fits. Reads update recency. Replacing
a key does not evict other entries unless the replacement needs more space.

`Delete` compiles its pattern as a Go regular expression and removes every
matching key. Invalid patterns return an error. `Clear` removes every entry in
the backing store, so a custom store should be dedicated to the cache.

The in-process store copies byte slices on reads and writes. Expired entries
are pruned automatically at their nearest expiration and also before writes;
`Get` checks expiration as well. The default store is process-local and not
durable. Custom stores control their own eviction and expiration policies.
Values must be GOB-encodable; GPP classes that need generated serialization
should use `@encoding.Serializable`.
