# rollfuse OpenFeature provider (Go)

Use [OpenFeature](https://openfeature.dev)'s vendor-neutral Go SDK against
[rollfuse](https://rollfuse.com) — evaluate flags through the standard
`openfeature.Client` API, backed by [`@rollfuse/go-sdk`](https://github.com/rollfuse/go-sdk)'s
local, deterministic evaluation underneath. Same zero-network-call-per-check
behavior as using the SDK directly; this package is a thin adapter, not a
different evaluation engine.

```bash
go get github.com/rollfuse/openfeature-provider
go get github.com/open-feature/go-sdk
```

```go
import (
	"context"

	"github.com/open-feature/go-sdk/openfeature"
	rollfuse "github.com/rollfuse/go-sdk"
	rollfuseprovider "github.com/rollfuse/openfeature-provider"
)

client, err := rollfuse.NewClient("https://api.rollfuse.com", os.Getenv("ROLLFUSE_SERVICE_CREDENTIAL"))
if err != nil {
	// handle error
}

if err := openfeature.SetProviderAndWait(rollfuseprovider.New(client)); err != nil {
	// handle error — blocks until the first Configuration fetch succeeds
}
defer openfeature.Shutdown()

ofClient := openfeature.NewDefaultClient()

enabled, err := ofClient.BooleanValue(context.Background(), "checkout-redesign", false,
	openfeature.NewEvaluationContext("user_123", map[string]any{"plan": "enterprise"}))
```

Every OpenFeature call site from here on is vendor-neutral: `BooleanValue`,
`StringValue`, `FloatValue`, `IntValue`, `ObjectValue`, and their
`*Details` variants all work, and switching providers later — evaluating
locally with a mock, or moving to a different flag system — never touches
your call sites, only the one `SetProviderAndWait` line.

## Why go through OpenFeature instead of `@rollfuse/go-sdk` directly

- **A standard interface your team may already use.** If other services
  in your organization already use OpenFeature against a different flag
  system, this lets rollfuse slot in without introducing a second
  evaluation API to learn.
- **Vendor portability.** `openfeature.Client` is the same interface
  regardless of which `FeatureProvider` is behind it — a real hedge
  against lock-in, not a marketing claim, since the OpenFeature Go SDK is
  a CNCF project maintained independently of rollfuse.
- **OpenFeature's hook/event ecosystem** (logging, metrics, tracing
  integrations built for OpenFeature generically) works with this
  provider for free.

If you don't need either of those, `@rollfuse/go-sdk` directly is simpler
— one fewer dependency, one fewer layer of indirection, and the same
underlying evaluation.

## Evaluation context mapping

| OpenFeature | rollfuse |
|---|---|
| `targetingKey` | The flag's subject key (`client.Evaluate(subjectKey, ...)`) — **required**; missing it resolves to your default value with error code `TARGETING_KEY_MISSING`, per the OpenFeature spec |
| Every other string-valued context attribute | An evaluation attribute (`rollfuse.WithAttributes(...)`) — matched by rules via strict string equality, same as calling the SDK directly |
| A non-string context attribute (number, bool, nested object) | **Excluded**, not stringified — a rule condition written for the string `"true"` should never accidentally match the boolean `true` because both got coerced to the same text |

## Error code mapping

| Condition | OpenFeature `ErrorCode` | `Reason` |
|---|---|---|
| No `targetingKey` in the evaluation context | `TARGETING_KEY_MISSING` | `ERROR` |
| No Configuration cached yet (`rollfuse.ErrConfigNotReady`) | `PROVIDER_NOT_READY` | `ERROR` |
| Flag key not found in the cached Configuration (`rollfuse.ErrFlagNotFound`) | `FLAG_NOT_FOUND` | `ERROR` |
| The flag's variation value doesn't decode into the requested type | `TYPE_MISMATCH` | `ERROR` |
| A rule matched | — | `TARGETING_MATCH` |
| Flag disabled, default served | — | `DISABLED` |
| No rule matched (or a matched rule's outcome couldn't resolve), default served | — | `DEFAULT` |

This provider deliberately never passes `rollfuse.WithFallback` to the
underlying client — see `resolve`'s doc comment in `provider.go` for why:
go-sdk's own fallback option is designed to make "config not ready" and
"flag not found" *never* return an error, which would make it impossible
for this provider to ever report `PROVIDER_NOT_READY`/`FLAG_NOT_FOUND` —
a real OpenFeature spec requirement. Every `*Evaluation` method already
receives its own `defaultValue` from the OpenFeature client; that's the
one this provider falls back to, with the correct error code attached.

## Development

```bash
go build ./...
go vet ./...
gofmt -l .
golangci-lint run ./...
go test ./... -race
```

No dependency beyond `@rollfuse/go-sdk` and `github.com/open-feature/go-sdk`
(and, transitively, whatever those two pull in).

## Related

- [`rollfuse/go-sdk`](https://github.com/rollfuse/go-sdk) — the SDK this
  provider wraps; use it directly if you don't need OpenFeature's
  vendor-neutral interface.
- [OpenFeature](https://openfeature.dev) — the CNCF spec and Go SDK this
  package implements a `FeatureProvider` for.
