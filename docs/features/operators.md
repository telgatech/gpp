# Operators

GPP supports Go's arithmetic, comparison, logical, and bitwise operators, as
well as compound assignments and postfix increment/decrement statements.
Operators follow Go's precedence and type rules. In particular, `^` means
bitwise XOR, not exponentiation; use `math.Pow` for floating-point powers.

## Compound assignments

The compound assignment operators are `+=`, `-=`, `*=`, `/=`, `%=`, `&=`, `|=`,
`^=`, `<<=`, `>>=`, and `&^=`:

```go
count, mask, bits := 10, 1, 0
count += 2
count -= 1
count *= 2
count %= 4
bits |= mask
bits <<= 1
bits &^= mask
```

Bitwise and shift assignments operate on integer values. `++` and `--` are
standalone statements, following Go syntax:

```go
count++
count--
```

They are not expressions, so `x = count++` is invalid. To save the old value,
assign it first and then increment:

```go
x = count
count++
```

## Assign if false or nil

`||=` assigns a default when a boolean is false or a nilable value is nil. The
right side runs only when the left side needs a value:

```go
var enabled bool
enabled ||= loadEnabled()

class Service {}
var service *Service
service ||= Service()
```

## Related expression features

GPP also adds [safe access](/features/safe-access) with `?.`, [lazy error
fallback](/features/error-fallback) with `??`, and [conditional
expressions](/features/conditional-expressions) with `?:`. Each page explains
its evaluation rules and examples.
