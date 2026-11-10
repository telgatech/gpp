# Go++ Cron Standard Library Specification

## Status

Initial implementation specification.

## Summary

`gpp/cron` schedules annotated top-level functions and instance methods.
Top-level functions use the package scheduler. A class derived from
`cron.Cron` groups instance methods into an independent scheduler, so its jobs
can share the instance's dependencies and state.

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

- Declare scheduled work beside an ordinary function or class method.
- Bind scheduled methods to their owning class instance.
- Keep startup explicit so importing a package never starts background jobs.
- Use the standard Go `context.Context` cancellation model.
- Keep generated code readable and ordinary Go compatible.
- Provide common cron expressions and a typed duration-based interval form
  without requiring a third-party package.
- Make failures, overlap behavior, and shutdown observable and predictable.

## Non-goals

- Distributed scheduling, leader election, or cross-process locks.
- Persistent job state, retries, delivery guarantees, or a job queue.
- An HTTP server dependency or scheduler startup during package initialization.
- Replacing Go's goroutines, timers, or scheduler.
- A promise that a scheduled job runs exactly once.

## Annotation

```go
annotation Cron(expression string) on function, method
annotation Every(interval time.Duration) on function, method
```

`Cron` accepts a five-field calendar expression. `Every` accepts a positive
`time.Duration`, such as `time.Second` or `5 * time.Minute`. The standard
library declaration imports Go's `time` package for that annotation type.
Both annotations require this signature:

```go
func JobName(ctx context.Context) error
```

An annotated function must be top-level. An annotated method must belong to a
class that extends `cron.Cron` to be discovered by that instance's scheduler.
Both must be non-generic. `Cron` receives a string expression, which `Start()`
validates before launching any jobs.

## Registration and startup

The compiler emits generic runtime metadata for annotated functions and
instance methods, including typed invocation adapters. It does not emit
Cron-specific registration calls. At startup, `gpp/cron` walks that metadata,
selects `Cron` and `Every` annotations, and records matching jobs; it never
starts a goroutine during package initialization. Go package initialization
completes before `main`, so the registry includes annotations from imported
Go++ packages when `cron.Start()` is called.

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

## Instance job groups

`cron.Cron` is a base class for a scheduler owned by one application object.
Its `Start` method walks the runtime class's method descriptors and binds each
annotated callback to that concrete instance before starting the scheduler:

```gpp
class Maintenance : cron.Cron {
    DB *sql.DB

    func PruneExpired(ctx context.Context) error @{cron.Cron("0 2 * * *")} {
        return this.DB.PruneExpired(ctx)
    }

    func RefreshStats(ctx context.Context) error @{cron.Every(10 * time.Minute)} {
        return this.DB.RefreshStats(ctx)
    }
}

maintenance := Maintenance(DB: db)
maintenance.Start()
maintenance.Stop()
```

Each instance has its own registrations and running state. Calling `Start`
again on the same instance is idempotent; after `Stop` completes, it can be
started again. Method annotations do not add jobs to the package-level
scheduler. `Stop()` waits with `context.Background()`; use
`StopWithContext(ctx)` with a deadline when shutdown must be bounded.

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
- Call `cron.Stop()` to cancel jobs and wait for running functions to return;
  it uses `context.Background()`. Use `cron.StopWithContext(ctx)` to limit the
  wait; if it expires, the method returns `ctx.Err()` and cancellation remains
  in effect.
- Functions should observe `ctx.Done()` during long-running work so shutdown can
  finish promptly.

## API

```go
type JobFunc func(context.Context) error

var ErrInvalidSchedule error

func Start() error
func Stop() error
func StopWithContext(ctx context.Context) error

class Cron {
    func Start() error
    func Stop() error
    func StopWithContext(ctx context.Context) error
}
```

The package functions control the global scheduler for annotated top-level
functions. The `Cron` class methods control one instance scheduler.

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
the handler should observe its context. `Stop()` waits for all scheduler and
job goroutines with a background context; use `StopWithContext(ctx)` to bound
that wait.
Errors and panics from dynamic jobs follow the same logging policy as annotated
jobs. Different jobs may run concurrently, while each individual registration
continues to skip overlapping invocations.

The dynamic API does not change annotation behavior: `Cron` and `Every` remain
the preferred form for fixed schedules declared in source.

## Diagnostics

- Invalid function or method signatures are returned by `Start`.
- Instance methods are discovered when their class derives from `cron.Cron`.
- Invalid schedule expressions are returned by `Start` with the job name.
- Duplicate registered job names are returned by `Start`.
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
2. The compiler emits generic callable metadata for annotated functions and
   methods; `gpp/cron` discovers `Cron` and `Every` at runtime.
3. Importing an annotated package does not run jobs before `Start`.
4. `Start` validates all schedules before launching any worker.
5. `Every` jobs execute repeatedly and receive cancellation through context.
6. Calendar expressions support wildcard, list, range, and step fields with
   the documented ranges and day matching behavior.
7. A running job is not overlapped; errors and panics are logged.
8. `Stop` cancels scheduler loops and waits for active jobs subject to the
   caller's context deadline.
9. Each `cron.Cron` instance has isolated registrations and lifecycle state;
   `Start` binds annotated methods to that instance through runtime metadata.
10. Planned: `Add` starts a dynamic job immediately when called on a running
   scheduler and returns an opaque removable handle.
