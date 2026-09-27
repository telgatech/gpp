# YAML encoding

YAML is useful for configuration and other data that people need to read or edit directly. The `gpp/encoding` package provides generic helpers backed by Go's YAML v3 implementation.

## Side by side: load configuration

With the underlying YAML package, the caller prepares a destination:

```go
var config Config
if err := yaml.Unmarshal(data, &config); err != nil { return err }
```

With Go++'s typed helper:

```go
config, err := encoding.FromYAML[Config](data)
if err != nil { return err }
```

To write a value as YAML:

```go
data, err := encoding.ToYAML(config)
if err != nil { return err }
fmt.Println(string(data))
```

## Configuration examples

Load a file into a typed configuration class and apply defaults after decoding:

```go
class AppConfig {
	Host string
	Port int
}

config, err := encoding.FromYAML[AppConfig](contents)
if err != nil { return fmt.Errorf("read app config: %w", err) }
if config.Host == "" { config.Host = "127.0.0.1" }
if config.Port == 0 { config.Port = 8080 }
```

You can also emit a small diagnostic document without defining a class:

```go
data, err := encoding.ToYAML(record(service: "catalog", ready: true))
if err != nil { return err }
fmt.Print(string(data))
```

The encoder and decoder return errors directly, so callers can report invalid configuration or serialization failures with useful context. Use JSON instead when compatibility with web APIs and browser tooling is the primary concern.

See the [serialization specification](/reference/specifications/serialization) for shared serialization behavior.
