# Go++ Cron Standard Library Specification

## Status

Initial implementation specification.

## Summary

`gpp/cron` schedules package-level Go++ functions annotated with `cron.Cron`.
The compiler emits registration for each annotated function. Registration does
not start work: the application explicitly calls `cron.Start()` after its
dependencies and configuration are ready, then calls `cron.Stop(ctx)` during
shutdown.

```go
import (
	"context"
	cron "gpp/cron"
)

func PruneExpired(ctx context.Context) error @{cron.Cron("0 2 * * *")} {
	return deleteExpiredRecords(ctx)
}

func main() {
	cron.Start()
	startApplication()
}
```

This is an in-process scheduler. It does not replace an operating-system or
cloud scheduler for work that must be coordinated across replicas or survive a
process restart.

## Goals

- Declare scheduled work beside an ordinary top-level function.
- Keep startup explicit so importing a package never starts background jobs.
- Use the standard Go `context.Context` cancellation model.
- Keep generated code readable and ordinary Go compatible.
- Provide common cron expressions and a typed duration-based interval form
  without requiring a third-party package.
- Make failures, overlap behavior, and shutdown observable and predictable.

## Non-goals

- Distributed scheduling, leader election, or cross-process locks.
- Persistent job state, retries, delivery guarantees, or a job queue.
- An HTTP server dependency or an implicit class instance.
- Replacing Go's goroutines, timers, or scheduler.
- A promise that a scheduled job runs exactly once.

## Annotation

```go
annotation Cron(expression string) on function
annotation Every(interval time.Duration) on function
```

`Cron` accepts a five-field calendar expression. `Every` accepts a positive
`time.Duration`, such as `time.Second` or `5 * time.Minute`. The standard
library declaration imports Go's `time` package for that annotation type.
Both annotations are valid on top-level functions only. The first
implementation requires this signature:

```go
func JobName(ctx context.Context) error
```

The function must be package-level and cannot be a method, generic function, or
function literal. The expression must be a string literal so invalid or
unsupported syntax is visible in source and can be validated by `Start()`.

## Registration and startup

For each annotated function, the compiler emits an initialization-time call to
the appropriate `gpp/cron` registration hook. That hook records the function
and its schedule; it never starts a goroutine. Go package initialization completes
before `main`, so the registry includes annotated functions from imported Go++
packages when `cron.Start()` is called.

`cron.Start() error` validates every registered schedule before starting any
job. If one expression is invalid, `Start` wraps `cron.ErrInvalidSchedule` and
starts none of the jobs. Go++ callers can identify this specific error with
`errors.Is` inside a catch block. It starts the scheduler in the background and
returns immediately.
Calling `Start` again while it is running is idempotent. After a completed
`Stop`, it may be started again.

Applications should call `Start` after configuration and required services are
ready. An imported package's annotated jobs become active only when the
application calls `Start`.

## Schedule expressions

The initial cron expression format has five numeric fields:

```text
minute hour day-of-month month day-of-week
```

Fields support `*`, comma-separated values, inclusive ranges, and positive
steps, for example:

```text
*/15 * * * *       every 15 minutes
0 2 * * *          daily at 02:00
30 8 * * 1-5       weekdays at 08:30
0 0 1,15 * *       first and fifteenth day of each month
```

The ranges are minute `0-59`, hour `0-23`, day-of-month `1-31`, month `1-12`,
and weekday `0-6` (Sunday is `0`). Month and weekday names, seconds, time-zone
prefixes, and named macros are not part of the initial format. Calendar
schedules use the process's local time zone.

If both day-of-month and day-of-week are restricted, a date matches when either
field matches. If one is `*`, both field checks must match, following common
five-field cron behavior.

The `Every` annotation accepts a positive Go `time.Duration` and schedules
after each interval:

```go
func RefreshCache(ctx context.Context) error @{cron.Every(5 * time.Minute)}
```

Intervals must be positive. Missed calendar times are skipped; the scheduler
does not replay missed invocations after downtime.

## Execution behavior

- Each job receives a context derived from the scheduler's cancellation
  context.
- A job does not overlap with itself. If its next scheduled time arrives while
  it is still running, that invocation is skipped and logged.
- Different jobs may run concurrently.
- Returned errors and recovered panics are logged with the job name; they do
  not stop other jobs or retry automatically.
- An application must call `cron.Stop(ctx)` to cancel jobs and wait for running
  functions to return. If `ctx` expires first, `Stop` returns `ctx.Err()` and
  cancellation remains in effect.
- Functions should observe `ctx.Done()` during long-running work so shutdown can
  finish promptly.

## API

```go
type JobFunc func(context.Context) error

var ErrInvalidSchedule error

func Start() error
func Stop(ctx context.Context) error
```

The compiler-generated `Register` hook is exported for generated Go code but
is not the normal application API. Applications should use annotations and
`Start`/`Stop`.

## Planned: dynamic jobs

The package is planned to support jobs added with lambdas as well as
compile-time annotations. This is useful when a schedule or its callback is
assembled from runtime configuration, or when the callback closes over an
application service:

```go
job := cron.Add("0 2 * * *", func(ctx context.Context) error {
	return cleanupExpiredSessions(ctx)
})

// Later, stop future runs for this registration.
cron.Remove(job)
```

The planned API is:

```go
type JobHandle struct{} // opaque; fields are unexported

func Add(expression string, function JobFunc) (JobHandle, error)
func Remove(handle JobHandle) error
```

`Add` will accept the documented five-field cron expressions and `@every`
duration expressions. It validates the schedule and callback before returning.
Jobs added before `Start` begin with the other registered jobs. Jobs added
while the scheduler is running begin scheduling immediately.
In Go++, an unhandled trailing error from `Add` or `Remove` is promoted to an
exception.

`JobHandle` is an opaque identifier for one registration. `Remove` is
idempotent, stops future invocations, and cancels the context of an invocation
already in progress. It returns without waiting for that invocation to finish;
the handler should observe its context. `Stop(ctx)` remains the operation that
waits for all scheduler and job goroutines to exit, subject to its deadline.
Errors and panics from dynamic jobs follow the same logging policy as annotated
jobs. Different jobs may run concurrently, while each individual registration
continues to skip overlapping invocations.

The dynamic API does not change annotation behavior: `Cron` and `Every` remain
the preferred form for fixed schedules declared in source.

## Diagnostics

- Invalid function signatures are compile-time errors.
- Invalid schedule expressions are returned by `Start` with the job name.
- Duplicate registered job names are initialization errors.
- Job errors, panics, and skipped overlapping runs are logged.

## Security and operations

Scheduled functions run with the process's permissions. They are ordinary
program code, not sandboxed jobs. Schedule them once per process only when that
matches the deployment topology. Multiple application replicas each run their
own copy of every registered job.

## Example with graceful shutdown

```go
import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
	cron "gpp/cron"
)

func Compact(ctx context.Context) error @{cron.Cron("0 3 * * *")} {
	return compactDatabase(ctx)
}

func main() {
	cron.Start()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cron.Stop(shutdown)
}
```

## Acceptance criteria

1. `gpp/cron` is a bundled official package and is discoverable by compiler,
   documentation, and language tooling.
2. The compiler validates `Cron` and `Every` as function annotations and emits
   registration for valid top-level Go++ functions.
3. Importing an annotated package does not run jobs before `Start`.
4. `Start` validates all schedules before launching any worker.
5. `Every` jobs execute repeatedly and receive cancellation through context.
6. Calendar expressions support wildcard, list, range, and step fields with
   the documented ranges and day matching behavior.
7. A running job is not overlapped; errors and panics are logged.
8. `Stop` cancels scheduler loops and waits for active jobs subject to the
   caller's context deadline.
9. Planned: `Add` starts a dynamic job immediately when called on a running
   scheduler and returns an opaque removable handle.
