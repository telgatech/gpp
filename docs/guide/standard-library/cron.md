# `gpp/cron`

`gpp/cron` schedules top-level functions with a `Cron` annotation. An annotation
declares when a job should run; `cron.Start()` explicitly starts all registered
jobs, and `cron.Stop(ctx)` cancels them during shutdown. The compiler generates
the registration code, so you do not need to construct job classes or maintain
a separate list of function callbacks.

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

```go [Go++]
import (
	"context"
	"time"
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

The Go++ function describes the job and its timing once. The compiler records
it during package initialization, but nothing runs until `cron.Start()` is
called. The scheduler invokes jobs in the background and logs returned errors.

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

## Start and stop the scheduler

Call `Start` after configuration and dependencies are ready. It validates all
registered schedules before starting any of them, then returns immediately.
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
	app.Listen()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cron.Stop(ctx)
}
```

Jobs should observe `ctx.Done()` when doing long-running work so shutdown can
finish promptly. If the shutdown context expires, `Stop` returns its error and
the job context remains canceled.

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
Go++. Call `Stop(ctx)` when the application needs to wait for all active work
to finish.

::: warning Dynamic job registration is planned

The current package supports annotated functions and the `Start`/`Stop`
lifecycle. `cron.Add` and `cron.Remove` are specified but are not implemented
yet.

:::

## Job behavior

- Different jobs can run at the same time.
- A single job never overlaps itself. If it is still running at its next
  scheduled time, that run is skipped and logged.
- Returned errors and panics are logged with the function name. They do not
  stop other jobs, and the package does not retry them.
- Imported Go++ packages can contribute registered jobs, but importing them
  alone never starts the scheduler. Calling `Start` starts every registered
  annotated function in the program.
- Jobs run in each process independently. Multiple replicas will each run the
  same schedules; use a platform scheduler or distributed lock when only one
  replica should perform a task.

## Function requirements

For now, an annotated function must be a top-level, non-generic function with
this signature:

```go
func Job(ctx context.Context) error
```

The annotation must use a string literal. `Start` reports invalid schedule
expressions with the job name. The current parser supports numeric fields and
does not include month/day names, seconds, time-zone prefixes, or named
shortcuts.

## Specification

- [Cron standard-library specification](/reference/specifications/std.cron)
