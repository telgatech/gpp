# Go++ Feature Spec: Lambda Expressions

## Goal

Add lightweight lambda syntax as sugar for ordinary Go function literals.

Primary motivation:

Make callbacks for collection helpers and other higher-order APIs concise.

Example:

users.Any(user => user.Active)

instead of:

users.Any(func(user User) bool {
    return user.Active
})

Lambdas must lower to ordinary Go closures/function literals.

No new runtime mechanism is required.

## 1. Core syntax

Single parameter:

user => user.Active

Multiple parameters:

(a, b) => a.Name < b.Name

Zero parameters:

() => doSomething()

Block body:

user => {
    println(user.Name)
    return user.Active
}

## 2. Parameter parentheses

For exactly one parameter, parentheses are optional:

user => user.Active

This is preferred.

Also allow:

(user) => user.Active

For zero or multiple parameters, parentheses are required:

() => expr

(a, b) => expr

Do not allow:

a, b => expr

## 3. Expression body

A lambda may contain a single expression:

user => user.Active

This should infer an implicit return.

Equivalent Go:

func(user User) bool {
    return user.Active
}

Another example:

x => x * 2

Equivalent:

func(x int) int {
    return x * 2
}

## 4. Block body

A lambda may use a normal block:

user => {
    println(user.Name)

    if user.Disabled {
        return false
    }

    return user.Active
}

Block-body lambdas use ordinary Go++ statement and return rules.

No implicit return from the final statement in block form.

## 5. Contextual type inference

Lambda parameter types should normally be inferred from the expected function type.

Example:

func Any(fn func(User) bool) bool

Call:

users.Any(user => user.Active)

Compiler knows:

user : User
return type : bool

No explicit type is required.

## 6. Multiple parameter inference

Example:

func Sort(less func(User, User) bool)

Call:

users.Sort((a, b) => a.Name < b.Name)

Infer:

a : User
b : User
return : bool

## 7. Explicit parameter types

Allow explicit types when contextual inference is unavailable or desired.

Example:

(user User) => user.Active

Multiple:

(a User, b User) => a.Name < b.Name

Zero parameter lambdas remain:

() => ...

## 8. Explicit return type

Do not require explicit lambda return type in normal usage.

Return type should be inferred from:

- expected function type, when available
- expression body type
- block return statements

Optional explicit return type may be deferred unless there is a concrete use case.

Keep v1 syntax small.

## 9. Target function type

A lambda must resolve to a concrete function type.

Typical contextual cases:

users.Any(user => user.Active)

users.Filter(user => user.Active)

users.Sort((a, b) => a.Name < b.Name)

someFunction(callback => ...)

Assignment:

var pred func(User) bool

pred = user => user.Active

valid.

## 10. Standalone inference

For:

pred := user => user.Active

there may be insufficient information to determine `user` type.

Recommended v1 behavior:

reject unless parameter types are explicit.

Valid:

pred := (user User) => user.Active

Invalid:

pred := user => user.Active

Error:

cannot infer lambda parameter type `user`
provide an explicit type or use lambda in a typed context

## 11. Generic contextual inference

Example prelude method:

func Any(fn func(T) bool) bool

For:

users.Any(user => user.Active)

where:

users : []User

infer:

T = User

then:

user : User

Generic argument inference should happen before lambda body type-checking where necessary.

## 12. Closure capture

Lambdas use normal Go closure capture semantics.

Example:

limit := 100

expensive := users.Filter(user => user.Salary > limit)

Generated Go should capture `limit` exactly as an ordinary Go func literal would.

Do not invent separate capture semantics.

## 13. Mutation of captured variables

Follow ordinary generated Go semantics.

Example:

count := 0

users.Any(user => {
    count++
    return user.Active
})

Whatever semantics Go++ already defines for `count++` continue to apply.

## 14. Return inference for expression lambdas

Expression:

user => user.Active

has the type of:

user.Active

Example:

x => x.Name

could resolve to:

func(User) string

when expected.

## 15. Void expression lambdas

Allow expression lambdas returning no value when the expression is a call with no result.

Example:

users.ForEach(user => println(user.Name))

Equivalent:

func(user User) {
    println(user.Name)
}

If Go++ does not yet have ForEach, this still applies to any API expecting:

func(T)

## 16. Block return checking

For expected:

func(User) bool

this is invalid:

user => {
    println(user.Name)
}

because no bool is returned on all required paths.

Reuse ordinary Go++ function return-path checking.

## 17. Overload resolution

Lambda contextual typing may participate in overload resolution.

Example:

func Foo(fn func(User) bool)
func Foo(fn func(User) string)

Call:

Foo(user => user.Active)

should select:

func(User) bool

because body resolves to bool.

However, avoid circular or overly complex resolution.

Recommended approach:

1. collect viable overloads by arity
2. contextually type lambda against each candidate
3. retain candidates where lambda type-checks
4. apply normal overload ranking
5. ambiguity remains a compile error

## 18. Parser

Recommended forms:

Identifier `=>` Expr
Identifier `=>` Block

`(` ParamList? `)` `=>` Expr
`(` ParamList? `)` `=>` Block

Conceptual grammar:

LambdaExpr
    <- SingleLambdaParam "=>" LambdaBody
     / "(" LambdaParams? ")" "=>" LambdaBody

SingleLambdaParam
    <- Identifier

LambdaParams
    <- LambdaParam ("," LambdaParam)*

LambdaParam
    <- Identifier Type?

LambdaBody
    <- Block
     / Expr

Be careful to distinguish:

(x) => ...

from ordinary parenthesized expressions.

## 19. AST

Add:

type LambdaExpr struct {
    Params        []LambdaParam
    BodyExpr      Expr
    BodyBlock     *BlockStmt
    ExpectedType  Type
    ResolvedType  Type
}

type LambdaParam struct {
    Name string
    Type TypeExpr // optional in source
}

Exactly one of BodyExpr or BodyBlock is populated.

## 20. Semantic analysis

For contextually typed lambda:

1. determine expected function type
2. verify parameter count
3. infer unspecified parameter types
4. bind lambda parameters into local scope
5. type-check body
6. verify body return type against expected return type(s)
7. produce resolved function type

## 21. Multiple return values

Block lambdas may return multiple values if expected function type does.

Example:

func(T) (int, error)

lambda:

x => {
    return x.Id, nil
}

For expression-body lambdas, a single expression may naturally produce multiple Go values only if current Go++ expression semantics already support that.

Do not special-case unless necessary.

## 22. Variadic function types

Lambdas themselves do not need new variadic syntax initially.

If desired later:

(args ...any) => ...

may reuse ordinary parameter syntax.

Not required for v1.

## 23. `this` inside lambdas

Lambdas nested inside class methods should capture outer `this` normally.

Example:

class Users {
    Items []User

    func Active() []User {
        return Items.Filter(user => this.Enabled && user.Active)
    }
}

`this` refers to the enclosing method's receiver unless shadowed by existing language rules.

The lambda itself does not introduce a new `this`.

## 24. `return` semantics

`return` inside a lambda returns from the lambda, not the enclosing function.

Example:

func Foo() {
    users.Any(user => {
        if user.Active {
            return true
        }

        return false
    })

    println("still in Foo")
}

This follows ordinary closure semantics.

## 25. `defer`

If block-body lambdas allow `defer`, it applies to the lambda invocation scope exactly as it would inside a generated Go function literal.

No special behavior.

## 26. Exceptions

If Go++ exceptions are implemented later, throwing from a lambda follows normal exception semantics.

Do not add lambda-specific exception handling.

## 27. Async/goroutines

Lambdas are ordinary function values and may be used where Go functions are used.

Example:

go (() => {
    work()
})()

Whether syntactic simplification is later added for goroutines is separate.

## 28. Generated Go

Input:

users.Any(user => user.Active)

Assuming user is User:

Generated conceptually:

users.Any(func(user User) bool {
    return user.Active
})

Input:

users.Sort((a, b) => a.Name < b.Name)

Generated:

users.Sort(func(a User, b User) bool {
    return a.Name < b.Name
})

Input:

users.Any(user => {
    println(user.Name)
    return user.Active
})

Generated:

users.Any(func(user User) bool {
    println(user.Name)
    return user.Active
})

## 29. No custom lambda runtime type

A lambda resolves directly to an ordinary Go function type.

Do not create:

Lambda<T>
Closure<T>
Callable

or any other custom runtime abstraction.

## 30. Go interoperability

Lambdas must be usable anywhere a normal Go function value is accepted.

Example:

http.HandleFunc("/", (w http.ResponseWriter, r *http.Request) => {
    fmt.Fprintln(w, "hello")
})

Generated Go should pass an ordinary compatible function literal.

No adapter should be necessary.

## 31. Prelude examples

This feature should immediately improve prelude usage.

Before:

users.Any(func(user User) bool {
    return user.Active
})

After:

users.Any(user => user.Active)

Before:

active := users.Filter(func(user User) bool {
    return user.Active
})

After:

active := users.Filter(user => user.Active)

Before:

users.Sort(func(a, b User) bool {
    return a.Name < b.Name
})

After:

users.Sort((a, b) => a.Name < b.Name)

Find:

user, ok := users.Find(user => user.Id == id)

All:

if users.All(user => user.Valid) {
    ...
}

## 32. No implicit `it`

Do not add implicit `it` as part of lambda support.

Prefer explicit concise parameters:

user => user.Active

over:

it.Active

This keeps nested lambdas and scopes obvious.

## 33. No placeholder syntax

Do not initially add:

_.Active
$0.Active
it.Active

or similar shorthand.

`=>` provides sufficient reduction in ceremony.

## 34. Formatting

Formatter should preserve concise expression lambdas:

user => user.Active

Multiple:

(a, b) => a.Name < b.Name

Block:

user => {
    println(user.Name)
    return user.Active
}

Do not unnecessarily expand expression lambdas into blocks.

## 35. Diagnostics

Cannot infer:

pred := user => user.Active

error:

cannot infer type of lambda parameter `user`

Use:

pred := (user User) => user.Active

Wrong parameter count:

expected func(User, User) bool

provided:

user => user.Active

error:

lambda expects 2 parameters from target function type, got 1

Wrong result:

users.Any(user => user.Name)

where Any expects bool:

error:

lambda result type mismatch
expected bool
got string

## 36. Suggested implementation order

1. Parse single-parameter expression lambdas.
2. Add LambdaExpr AST.
3. Contextually infer parameter type from expected func type.
4. Lower to Go function literal.
5. Add multiple parameters.
6. Add zero parameters.
7. Add block-body lambdas.
8. Add explicit parameter types.
9. Integrate generic inference.
10. Integrate overload resolution.
11. Add formatter support.
12. Add Go interop tests.
13. Add prelude callback tests.

## 37. Core tests

Single parameter:

func apply(fn func(int) int) int {
    return fn(2)
}

x := apply(v => v * 2)

x == 4

Multiple:

func compare(fn func(int, int) bool) bool {
    return fn(1, 2)
}

compare((a, b) => a < b)

Zero:

func run(fn func()) {
    fn()
}

run(() => println("hi"))

Block:

users.Any(user => {
    println(user.Name)
    return user.Active
})

Explicit parameter:

pred := (user User) => user.Active

Capture:

limit := 10
xs.Filter(x => x > limit)

Go interop:

http.HandleFunc("/", (w http.ResponseWriter, r *http.Request) => {
    fmt.Fprintln(w, "ok")
})

## 38. Design principle

Go++ lambdas are only concise syntax for Go function literals.

They should remove:

func(parameter Type) ReturnType {
    return expression
}

when the surrounding type already provides that information.

The core form:

parameter => expression

should make higher-order APIs practical without introducing a new function model or runtime abstraction.
