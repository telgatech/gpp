# `gpp/exp/workflow`

`gpp/exp/workflow` provides durable state machines on top of the experimental
work queue. `workflow.Message` extends `queue.Message` and implements workflow
execution on each queue attempt. The queue handles generic message execution
outcomes and does not inspect workflow state. It stores the current state and
fields in an append-only file and starts processing automatically.

## Define a state machine

Declare states and transitions as data. `_execute(state)` performs one state's
work and returns an event name; the transition table uses that event to select
the next state. This keeps branching rules outside the action code.

```gpp
import (
    "fmt"
    "time"
    queue "gpp/exp/queue"
    workflow "gpp/exp/workflow"
)

class GenerateReport : workflow.Message {
    ReportID string
    Recipient string

    func _states() []workflow.State {
        return workflow.State[
            (Name: "generate-report"),
            (Name: "notify"),
            (Name: "done", Terminal: true),
        ]
    }

    func _transitions() []workflow.Transition {
        return workflow.Transition[
            (From: "generate-report", On: "generated", To: "notify"),
            (From: "notify", On: "sent", To: "done"),
        ]
    }

    func _execute(state string) (string, error) {
        switch state {
        case "generate-report":
            fmt.Println("generated report", this.ReportID)
            return "generated", nil
        case "notify":
            fmt.Println("notified", this.Recipient, "about report", this.ReportID)
            return "sent", nil
        default:
            return "", fmt.Errorf("unknown state %q", state)
        }
    }
}

func main() {
    q := queue.New()
    q.Push(GenerateReport(ReportID: "weekly-summary", Recipient: "ada@example.com"))
    q.Drain(5 * time.Second)
}
```

The queue runs `generate-report`, checkpoints the new state, then runs
`notify`. When that step returns `sent`, the message reaches `done` and the
queue acknowledges it. The report ID, recipient, and current state are
serialized with the message, so unfinished work can resume after a restart.

## Branches, waits, and retries

- `_states()` declares valid states; the first entry is the initial state.
  Mark the terminal state with `Terminal: true`. A state can override queue
  retries with `Retries` and `HasRetries: true`.
- `_transitions()` maps a `(From, On)` state and event pair to a target state.
  Each pair must be unique.
- `_waits()` pauses a state until the named signal arrives. A wait can also set
  a positive `Timeout` and `TimeoutEvent`; that event follows an ordinary
  transition. The worker is free to process other messages while this one
  waits.
- `_execute(state)` runs one state's action and returns its event. The runtime
  checkpoints the new state before dispatching the next action.

The queue starts automatically. `PushID` returns the durable ID needed by
`Signal`; `Drain(timeout)` waits for active work to finish. See the
[queue guide](./exp-queue) for retries, dead letters, priorities, and shutdown.

## Durability

The queue syncs each successful transition and the updated serialized message
before running the next state. A process restart resumes at the last checkpoint.
Delivery is at least once: if a process fails after an external side effect but
before its checkpoint is synced, the state may run again. Make actions safe to
retry or use idempotency keys for external operations.

Workflow state and event names, plus serialized fields, must remain compatible
while queued messages may still need to resume. The queue is single-process;
do not open the same queue file from multiple processes or queue instances.

Run the [workflow example](https://github.com/telgatech/gpp/blob/main/examples/workflow.gpp)
with `seed` to see its sample orders. Its steps finish quickly, so interrupting
it manually is not a reliable way to observe crash recovery. The test simulates
a worker-process crash after durable checkpoints, then restarts the queue from
the same file and verifies that the workflow resumes:

```sh
gpp run examples/workflow.gpp -- seed
gpp test tests/workflow.gpp
```
