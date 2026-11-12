# `gpp/cron`

`gpp/cron` schedules top-level functions or methods annotated with `Cron` or
`Every`. Top-level jobs use the package scheduler. For related jobs that share
dependencies and state, derive a class from `cron.Cron`; each instance owns an
independent scheduler and binds its annotated methods to that instance.

## Why use it

Go already provides timers and tickers, and external cron packages provide
calendar expressions. The application still has to keep the callback registry,
start the scheduler, handle cancellation, and decide what happens when a job
fails or runs long. `gpp/cron` puts the schedule beside the function and defines
those runtime behaviors while keeping startup under your control.

## Side by side: schedule a recurring job

With ordinary Go, an interval loop is usually started manually and must handle
its own cancellation and errors:

::: code-group
```go [Go]
import (
	"context"
	"log"
	"time"
)

func Refresh(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := refreshCache(ctx); err != nil {
				log.Printf("refresh failed: %v", err)
			}
		}
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Refresh(ctx)
	serveRequests()
}
```

```go [GPP]
import (
	cron "gpp/cron"
)

func Refresh(ctx context.Context) error @{cron.Every(5 * time.Minute)} {
	return refreshCache(ctx)
}

func main() {
	cron.Start()
	serveRequests()
}
```
:::

The annotation and callback are recorded as runtime metadata during package
initialization, but nothing runs until `cron.Start()` is called. The scheduler
invokes jobs in the background and logs returned errors.

## Calendar schedules

Use a five-field expression for calendar schedules:

```go
import (
	"context"
	cron "gpp/cron"
)

func PruneExpired(ctx context.Context) error @{cron.Cron("0 2 * * *")} {
	return deleteExpiredRecords(ctx)
}

func SendWeeklyDigest(ctx context.Context) error @{cron.Cron("30 8 * * 1")} {
	return sendDigest(ctx)
}
```

The fields are minute, hour, day of month, month, and weekday. Fields support
`*`, lists, ranges, and positive steps. For example, `*/15 * * * *` runs every
15 minutes, while `30 8 * * 1-5` runs at 08:30 on weekdays. The scheduler uses
the process's local time zone, and missed runs are skipped rather than replayed.

For interval schedules, use `cron.Every` with a Go `time.Duration`. This keeps
the interval typed and avoids encoding it in a string:

```go
import (
	"context"
	"time"
	cron "gpp/cron"
)

func RefreshIndex(ctx context.Context) error @{cron.Every(45 * time.Second)} {
	return rebuildSearchIndex(ctx)
}
```

## Group jobs on a class

Derive a class from `cron.Cron` when its scheduled jobs share services or state.
`Start` walks the instance's runtime method metadata, finds `Cron` and `Every`
annotations, and binds their generated typed invokers to that instance. Each
scheduler keeps its own job registrations:

```gpp
import (
    "context"
    "fmt"
    "time"
    cron "gpp/cron"
)

class Maintenance : cron.Cron {
    Runs int

    func Heartbeat(ctx context.Context) error @{cron.Every(time.Second)} {
        this.Runs++
        fmt.Println("scheduled run", this.Runs)
        return nil
    }

    func Weekday(ctx context.Context) error @{cron.Cron("*/15 9-17 * * 1-5")} {
        fmt.Println("weekday maintenance")
        return nil
    }
}

maintenance := Maintenance()
maintenance.Start()
// during shutdown:
maintenance.Stop()
```

An annotated method has the same `func(context.Context) error` signature as a
top-level job and must be on a class that extends `cron.Cron`. The base class
provides `Start`; the package-level scheduler remains useful for simple jobs
that do not need an owning instance.

## Start and stop the scheduler

Call `Start` after configuration and dependencies are ready. It validates all
registered schedules before starting any of them, then returns immediately.
An invalid schedule makes `Start` wrap `cron.ErrInvalidSchedule`, so GPP
applications can identify that specific startup failure in the existing catch
with `errors.Is`:

```gpp
func main() {
    try {
        cron.Start()
        app := App()
        app.Listen(0)
    } catch err {
        if errors.Is(err, cron.ErrInvalidSchedule) {
            fmt.Println("cron configuration is invalid")
        } else {
            fmt.Println("application:", err)
        }
    }
}
```

The context passed to each job is canceled by `Stop`:

```go
import (
	"context"
	"time"
	cron "gpp/cron"
)

func main() {
	cron.Start()
	app := App()
	app.Listen(0)
	cron.Stop()
}
```

`Stop()` waits using `context.Background()`. Use `StopWithContext(ctx)` when
shutdown needs a time limit. Jobs should observe their provided `ctx.Done()` so
shutdown can finish promptly; if the supplied shutdown context expires,
`StopWithContext` returns its error and the job context remains canceled.

## Planned: add and remove jobs at runtime

Annotations are a good fit for schedules fixed in source. A future dynamic API
will also let code add a lambda that captures application services, then
remove that registration by its handle:

```go
job := cron.Add("0 2 * * *", func(ctx context.Context) error {
	return cleanupExpiredSessions(ctx)
})

cron.Remove(job)
```

`Add` is planned to accept five-field cron expressions and `@every` durations.
Jobs added before `Start` will start with the scheduler; jobs added after
`Start` will begin scheduling immediately. `Remove` will stop future runs and
cancel an in-progress run's context, then return without waiting for the handler
to exit. Unhandled trailing errors from `Add` and `Remove` become exceptions in
GPP. `Stop()` uses a background context; use `StopWithContext(ctx)` when
shutdown needs a deadline.

::: warning Dynamic job registration is planned

The package supports annotated top-level functions, annotated methods on
`cron.Cron` subclasses, and package or instance `Start`/`Stop` lifecycles.
`cron.Add` and `cron.Remove` are specified but are not implemented yet.

:::

## Job behavior

- Different jobs can run at the same time.
- A single job never overlaps itself. If it is still running at its next
  scheduled time, that run is skipped and logged.
- Returned errors and panics are logged with the function name. They do not
  stop other jobs, and the package does not retry them.
- Imported GPP packages can contribute top-level registered jobs, but
  importing them alone never starts the package scheduler. Calling
  `cron.Start` starts every registered top-level function; calling an instance's
  `Start` starts only that class instance's annotated methods.
- Jobs run in each process independently. Multiple replicas will each run the
  same schedules; use a platform scheduler or distributed lock when only one
  replica should perform a task.

## Job handler requirements

An annotated top-level function or instance method must be non-generic and have
this signature:

```go
func Job(ctx context.Context) error
```

`cron.Start()` and an instance's `Start()` discover annotations from generic
runtime metadata. The compiler generates typed invocation adapters for
annotated functions and methods; it does not implement Cron scheduling rules.
Instance methods must be available on a class derived from `cron.Cron`.
`Cron` accepts a string expression. `Start` reports invalid signatures and
schedule expressions with the job name before starting the jobs. The current
parser supports numeric fields and does not include month/day names, seconds,
time-zone prefixes, or named shortcuts.

## Specification

- [Cron standard-library specification](/reference/specifications/std.cron)
