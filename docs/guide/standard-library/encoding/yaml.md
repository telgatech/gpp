# YAML encoding

YAML is useful for configuration and other data that people need to read or edit directly. The `gpp/encoding` package provides generic helpers backed by Go's YAML v3 implementation.

## Side by side: load configuration

The Go YAML API decodes into a destination pointer and returns errors explicitly. Go++ can declare the configuration as a class and infer the decoded value from a generic type argument; unhandled errors propagate automatically:

::: code-group

```go [Go++]
class AppConfig @{encoding.Serializable} {
	Host string `yaml:"host"`
	Port int `yaml:"port"`
}

func LoadConfig(data []byte) AppConfig {
	config := AppConfig.FromYAML(data)
	if config.Host == "" { config.Host = "127.0.0.1" }
	if config.Port == 0 { config.Port = 8080 }
	return config
}

func SaveConfig(config AppConfig) []byte {
	return config.ToYAML()
}
```

```go [Go]
var config Config
if err := yaml.Unmarshal(data, &config); err != nil {
	return err
}

encoded, err := yaml.Marshal(config)
if err != nil {
	return err
}
fmt.Println(string(encoded))
```

:::

## Configuration examples

Load a file into a typed configuration class and apply defaults after decoding:

```go
class AppConfig {
	Host string
	Port int
}

config := AppConfig.FromYAML(contents)
if config.Host == "" { config.Host = "127.0.0.1" }
if config.Port == 0 { config.Port = 8080 }
```

You can also emit a small diagnostic document without defining a class:

```go
data := encoding.ToYAML(record(service: "catalog", ready: true))
fmt.Print(string(data))
```

The encoder and decoder preserve errors from their underlying implementations; Go++ propagates uncaptured errors automatically. Use explicit handling when configuration failures need custom recovery or context. Use JSON instead when compatibility with web APIs and browser tooling is the primary concern.

See the [serialization specification](/reference/specifications/serialization) for shared serialization behavior.
