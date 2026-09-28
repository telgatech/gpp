# Polymorphism

Polymorphism lets a function work with a base class while the concrete value
supplies the behavior. It helps keep application code open to new types: a
caller can add another derived class without rewriting every function that
uses the base API.

## Compare interfaces with class inheritance

Go interfaces are an excellent way to define behavior. Go++ also lets a base
class provide shared implementation while derived classes specialize methods:

::: code-group
```go [Go++]
class Notifier {
    func Send(message string) string {
        return "default: " + message
    }
}

func Deliver(notifier Notifier, message string) string {
    return notifier.Send(message)
}
```

```go [Go]
type Notifier interface {
    Send(message string) string
}

func Deliver(notifier Notifier, message string) string {
    return notifier.Send(message)
}
```
:::

## Define a shared contract

A parent class declares behavior that derived classes can specialize:

```go
class Notifier {
    func Send(message string) string {
        return "default: " + message
    }
}

class EmailNotifier : Notifier {
    func Send(message string) string {
        return "email: " + message
    }
}

class SMSNotifier : Notifier {
    func Send(message string) string {
        return "sms: " + message
    }
}
```

The derived classes inherit the parent relationship and provide their own
implementation of `Send`.

## Accept the base type

Write the operation once against the parent class. Passing a derived value
preserves its concrete dispatch:

```go
func Deliver(notifier Notifier, message string) string {
    return notifier.Send(message)
}

email := EmailNotifier()
sms := SMSNotifier()

fmt.Println(Deliver(email, "Build finished")) // email: Build finished
fmt.Println(Deliver(sms, "Build finished"))   // sms: Build finished
```

This pattern is useful for notification backends, storage adapters, payment
providers, and other parts of an application where callers should depend on a
small shared API.

## Compatible with Go

Go++ generates the dispatch support needed for base-typed values. Go++ source
does not need a special runtime call at each method invocation, and ordinary
Go functions and libraries remain available at package boundaries.

Use a Go interface directly when an existing API already defines the right
contract. Use a Go++ base class when shared implementation or class
inheritance is useful. See the [inheritance and dispatch rules](/reference/specifications/base)
for how class values flow through variables, fields, parameters, and results.
