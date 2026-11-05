# cepvor-cpa-plinth

Shared SDK for CLIProxyAPI plugin development. Four production plugins
(codearts / qoder / trae / workbuddy) use it for the capabilities they all need.

**Why it exists.** `sdk/pluginapi` and `sdk/pluginabi` in CLIProxyAPI define
contract types only; they ship no implementation. The wire formats under
`internal/translator` live in an `internal` package, so a plugin can never
import them. This SDK fills that gap, plus a few plugin-side workarounds for
host gaps that cannot be fixed from outside.

## Packages

| Package | Responsibility |
| --- | --- |
| `abi` | Go side of the native plugin ABI: capability registration, method dispatch, typed host callbacks (`host.http.do` / `host.auth.*` / `host.log`), envelope construction |
| `auth` | Credential filenames must not be guessed (`file.go`); plugins check their own expiry and refresh instead of waiting for the host's 15-minute ticker (`refresh.go`) |
| `oauth` | Loopback callback listener. Transport only: bind a port, receive one path, hand the query to a waiter |
| `catalog` | Background periodic refresh of the model catalogue, including a workaround for the ABI's missing `model.refresh` method |
| `credjson` | Normalisation of string/number timestamps at the JSON boundary |
| `brands` | Provider icons for the panel, as data URLs, with no outbound requests |
| `config` | Typed coercion of plugin config values (bool / int / string / list / hours / selection) so the same rules cannot drift per plugin |
| `logging` | Structured plugin logging through `host.log`, plus a per-tick panic guard |
| `schedule` | Per-account jittered check-in slots and catch-up scheduling |

## What it does not provide

- **OpenAI / Anthropic wire formats.** Use a provider abstraction such as
  [`charm.land/fantasy`](https://github.com/charmbracelet/fantasy), which models
  reasoning as a first-class concern.
- **HTML management pages.** Plugins return JSON only; the Svelte panel that
  ships with CLIProxyAPI renders every screen.

## Usage

```go
import "github.com/Evsdrg/cepvor-cpa-plinth/abi"

func handleExecutorExecute(h *abi.Host, raw json.RawMessage) (any, error) {
    var req pluginapi.ExecutorRequest
    if err := abi.Decode(raw, &req); err != nil {
        return nil, abi.Errorf("bad_request", "decode: %v", err)
    }
    // ...
    return abi.OK(resp)
}
```

When developing a plugin and this SDK side by side, add a `replace` directive in
the plugin's `go.mod`:

```
replace github.com/Evsdrg/cepvor-cpa-plinth => ../cepvor-cpa-plinth
```

Released plugins should depend on a tagged version instead.

## Development

```bash
go test ./...
go vet ./...
```

## Compatibility

| | |
| --- | --- |
| Go | 1.26 |
| CLIProxyAPI | v8.0.32 or later |

## License

MIT — see [LICENSE](LICENSE).
