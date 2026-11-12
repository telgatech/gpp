# Conditional expressions

Use `condition ? whenTrue : whenFalse` when an expression needs to choose
between two values:

```gpp
label := enabled ? "on" : "off"
```

The condition must be `bool`, and both result expressions must resolve to a
compatible Go type. The compiler evaluates only the selected branch, so work in
the other branch is skipped:

```gpp
name := user != nil ? user.Name : loadDefaultName()
```

Conditional expressions can be nested. They associate from right to left:

```gpp
label := ready ? "ready" : connected ? "connecting" : "offline"
```

Use parentheses when they make a larger expression easier to read. Go does not
have this operator; GPP lowers it to ordinary Go code.
