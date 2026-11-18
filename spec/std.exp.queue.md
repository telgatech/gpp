# Experimental Work Queue

`gpp/exp/queue` provides a small durable work queue backed by an append-only
Gob log. It uses only the Go standard library and is intended for one process
and small to medium queues.

## API

```gpp
import "time"

class Message {
    Priority int
    func _execute() error
}

class Queue {
    static func Create(file string) (*Queue, error)
    static func CreateWithRetries(file string, retries int) (*Queue, error)
    func Push(message GppMessage) error
    func Dead() []*DeadLetter
    func Remove(item *DeadLetter) error
    func Purge() error
    func Drain(timeout time.Duration) error
    func Stop() error
}

func New(file string = "app.queue", retries int = 3) (*Queue, error)

class DeadLetter {
    ID uint64
    Priority int
    Retries int
    LastError string
    Message GppMessage
}

```

`New` creates the file and its parent directory if needed. The default path is
`./app.queue` in the current working directory, and the default is three retries
after the initial attempt. Set both constructor parameters by name when
configuring retries, for example `queue.New(file: "./app.queue", retries: 5)`.
Use a different path for each queue. Do not open the same queue file from
multiple processes or multiple queue instances at the same time.

## Messages and delivery

`queue.New` starts the worker automatically. Push messages as needed. Call
`Drain(timeout)` to stop accepting new work, wait for the queue to empty, and
close it. If the timeout expires, `Drain` returns an error and signals the
worker to stop after its current message execution; cleanup may finish after
`Drain` returns, and pending messages remain durable for the next queue
instance. Call `Stop()` to stop after the current message execution and wait for
that shutdown to finish. Each message subclass implements the protected
`_execute() error` hook; the worker calls it and acknowledges the message
automatically on success. A
larger `Priority` value runs before a smaller one; equal priorities run FIFO.
An omitted `Priority` defaults to `0`. `queue.Message` is serializable, and
subclasses inherit that behavior. The compiler generates
serialization for each concrete subclass, including its own exported fields
and inherited serializable fields:

```gpp
import "time"
import queue "gpp/exp/queue"

class EmailMessage : queue.Message {
    Recipient string
    Subject string

    func _execute() error {
        // Perform the work described by this message.
        return nil
    }
}

q := queue.New()
q.Push(EmailMessage(Priority: 5, Recipient: "dev@example.com", Subject: "Build complete"))
q.Drain(30 * time.Second)
```

`Push` serializes the message, appends a framed Gob event with a checksum, and
syncs it to disk before returning. Successful message executions append and sync
an acknowledgement event. A failed execution is logged and retried after a
short delay. `retries` is the number of additional attempts after the first;
after those attempts fail, the message is moved to the durable dead-letter set.
`Drain` waits for active messages to complete or become dead letters.
Unacknowledged work is available again after restart. Startup replays events to
rebuild the pending set and truncates an incomplete final record after a crash.
The log is compacted after it grows past 16 MiB and contains substantially more
history than live messages. On startup, any completed-message, dead-letter
removal, or purge history triggers compaction. The size threshold is also
checked at startup and after acknowledgments, dead-letter removals, and purges.

`Dead()` returns dead-letter entries in ID order without removing them. Each
entry includes its stable `ID`, retry count, last error, and original concrete
message in `Message`. Use `Remove(entry)` to delete one entry or `Purge()` to
delete all dead-letter entries. Both removals are recorded durably.

Delivery is at least once, so `_execute` should tolerate duplicates.
`Stop()` stops the worker after its current message execution and leaves
pending work in the log for the next queue instance.

Serializable message fields follow the `encoding.Serializable` GOB rules.
Gob is a Go-oriented local storage format, not a stable cross-language format.
Each normal state change appends only one event, though each event is synced
individually. The queue is not intended for multi-process workloads. Call
`Stop` to stop its consumer cleanly.
