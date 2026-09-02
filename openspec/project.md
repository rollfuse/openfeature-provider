# Project Context

## Product

**openfeature-provider** is the CNCF [OpenFeature](https://openfeature.dev)
`FeatureProvider` implementation for **rollfuse**, a developer-first
feature-flag and progressive-delivery platform. This repository holds
only the adapter — the platform itself (API, web console, the SDKs) lives
in `rollfuse/rollfuse`, `rollfuse/go-sdk`, and `rollfuse/js-sdk`.

## What This Package Does

Wraps an already-constructed `*rollfuse.Client` (from
[`github.com/rollfuse/go-sdk`](https://github.com/rollfuse/go-sdk)) to
satisfy `openfeature.FeatureProvider` and
`openfeature.ContextAwareStateHandler`
(`github.com/open-feature/go-sdk`), so applications already using
OpenFeature's vendor-neutral API can point it at rollfuse. No evaluation
logic lives here — every `*Evaluation` method translates an OpenFeature
call into exactly one `client.Evaluate` call and translates the result
back, including error codes and reasons.

## Engineering Priorities

1. Spec fidelity — every OpenFeature error code and Reason this provider
   can produce must be *correct* per the OpenFeature specification, not
   just plausible. A provider that reports the wrong error code is worse
   than one that under-reports, because callers branch on these codes.
2. No evaluation logic duplicated from `@rollfuse/go-sdk` — if a behavior
   question is "what does rollfuse do here," the answer belongs in
   go-sdk, not reimplemented or second-guessed in this adapter.
3. Maintainability
4. Performance (already inherited from go-sdk's zero-network-call
   evaluation; this package must not add a network call or measurable
   overhead of its own)

## Architecture Constraints

- No dependency beyond `github.com/rollfuse/go-sdk` and
  `github.com/open-feature/go-sdk` (and their own transitive deps).
- `Init`/`InitWithContext` must block until the wrapped client's first
  Configuration fetch succeeds (or the context/timeout is exceeded) —
  `openfeature.SetProviderAndWait` depends on this to make a caller's own
  "wait for the provider to be ready" contract meaningful.
- Never pass `rollfuse.WithFallback` to the wrapped client's `Evaluate`
  call — see `openspec/config.yaml`'s context for why (it would mask
  `FLAG_NOT_FOUND`/`PROVIDER_NOT_READY`, both real OpenFeature spec
  requirements this provider must be able to report).
- A non-string evaluation-context attribute is excluded from the
  attributes passed to go-sdk, never coerced to a string — see
  `provider.go`'s `stringAttributes` doc comment.

## Repository Shape

```text
/
├── provider.go        The whole implementation.
├── provider_test.go   Unit tests, one per Evaluation method + error path,
│                       using an httptest.Server the same way go-sdk's own
│                       tests do.
├── example_test.go    An Example test exercising this provider through
│                       the real, public openfeature.Client API — proof
│                       the whole integration works, not just the
│                       Provider struct in isolation.
├── .golangci.yml       Same linter config as rollfuse/go-sdk.
└── openspec/
```

## Specification Rules

- This repository has no `openspec/specs/` capability of its own — its
  correctness contract *is* the OpenFeature specification (external,
  versioned independently) plus go-sdk's own behavioral contract
  (`openspec/specs/sdk-go/spec.md` in `rollfuse/go-sdk`). A behavior
  question about evaluation itself belongs in go-sdk's spec, not this
  repo's.
- `openspec/changes/` here is for planning nontrivial changes to the
  adapter itself (a new error-code mapping, support for a new OpenFeature
  SDK feature) — not for specifying rollfuse platform behavior.
