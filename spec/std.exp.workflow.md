# Experimental Durable State Machines

## Purpose

`gpp/exp/workflow` provides state machines backed by `gpp/exp/queue`. A state
machine is a serializable queue message. Its fields and progress are persisted
in the queue's existing append-only file (by default, `./app.queue`). The queue
automatically executes ready states; clients do not receive messages manually.

The implementation is experimental, single-process, and at-least-once. It does
not promise exactly-once external side effects or distributed workers.

## Definition API

A concrete class subclasses `workflow.Message` and provides these protected
hooks. `workflow.Message` extends `queue.Message` with durable state-machine
execution.

- `_states() []workflow.State` declares names, terminal states, and optional
  per-state retry overrides. Its first entry is the initial state.
  `HasRetries` distinguishes an explicit zero retry limit from the queue
  default.
- `_transitions() []workflow.Transition` maps `(From, On)` state/event pairs to
  target state names.
- `_waits() []workflow.Wait` declares states that pause for a signal, with an
  optional positive timeout and timeout event.
- `_execute(state string) (event string, err error)` performs one state's
  action and returns the event used to look up the next transition.

State and event identifiers are strings. `workflow.Name[T](value)` converts a
string-backed enum or another stable value to a string key. The hook boundary
is not statically typed by state and event category; it is the implementation's
responsibility to use consistent keys. Names and serialized fields must remain
compatible while queued messages may still need to resume.

## Validation and Execution

Before dispatching a state action, the runtime validates the declared graph:
there must be at least one state; state names are nonempty and unique; the
first state is not terminal; transition endpoints exist and each
source/event pair is unique; wait states exist and are not terminal; and wait
signals/timeouts have matching transitions. State retry limits cannot be
negative.

The queue dispatches one state action at a time. On success, the returned event
must match a transition from the current state. The runtime persists the
updated serialized message and target state before dispatching another action.
A terminal target completes the queue message. `_execute` must not choose or
persist the target state itself; routing belongs in `_transitions()`.

When a machine enters a configured wait state, the queue checkpoints the state
and releases the worker. `Queue.Signal(id, event)` durably wakes that message
when `event` matches its configured signal. If the wait has a timeout, the
queue persists a due time and later emits the configured timeout event through
the same transition table. The ID comes from `Queue.PushID(message)`.

## Durability and Retries

The queue syncs each successful transition checkpoint before running the next
state. A restart replays the queue log and resumes at the last checkpoint. If a
process stops after an external side effect but before the checkpoint is
synced, that state can run again. State actions must be retry-safe or use
idempotency keys.

Failed states use the queue retry limit unless their `State` sets
`HasRetries: true`; successful transitions reset the retry counter for the next
state. When retries are exhausted, the machine is retained as a queue dead
letter with its most recent durable payload and error.

`Drain(timeout)` waits for ready messages to finish; workflows paused on an
external signal or a future timeout remain durable and are not considered
ready work. `Stop()` shuts down the worker while retaining unfinished messages.
Only one queue instance may open a given queue file at a time.

## Example and Tests

`examples/workflow.gpp` demonstrates order approval, branching, payment wait,
timeout cancellation, and normal completion. `tests/workflow.gpp` covers
signal resumption, timeout routing, per-state retries, and process-crash
recovery from the last checkpoint.
