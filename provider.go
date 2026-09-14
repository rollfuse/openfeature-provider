// Package rollfuseprovider implements the CNCF OpenFeature Go SDK's
// FeatureProvider interface (github.com/open-feature/go-sdk) backed by
// github.com/rollfuse/go-sdk, so an application already using OpenFeature
// (or wanting the vendor-neutral option of switching away from rollfuse
// later without touching call sites) can point openfeature.SetProvider at
// rollfuse without learning rollfuse's own Client API.
//
// Every evaluation still runs entirely against go-sdk's locally cached,
// versioned Configuration — no network call, no added latency — this
// package only translates between the two APIs' shapes: OpenFeature's
// per-type Evaluation methods and FlattenedContext on one side,
// go-sdk's Evaluate and EvaluationResult on the other.
package rollfuseprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	rollfuse "github.com/rollfuse/go-sdk"
)

// eventChannelCapacity bounds the buffered Event channel EventChannel
// returns. A slow or absent consumer draining it (openfeature.go-sdk's own
// event executor, normally) should never block trackConfigChange, which
// runs from the wrapped Client's own background poll goroutine — losing
// an occasional redundant ProviderConfigChange event under sustained
// backpressure is preferable to stalling Configuration polling.
const eventChannelCapacity = 16

// Provider adapts a *rollfuse.Client to openfeature.FeatureProvider (plus
// openfeature.ContextAwareStateHandler, so openfeature.SetProviderAndWait
// blocks until the first Configuration fetch succeeds, exactly like
// calling client.Start yourself would, and openfeature.EventHandler, so
// it emits ProviderConfigChange when the underlying Configuration changes
// — task 10.1).
type Provider struct {
	client *rollfuse.Client

	events      chan openfeature.Event
	unsubscribe func()
}

var (
	_ openfeature.FeatureProvider          = (*Provider)(nil)
	_ openfeature.ContextAwareStateHandler = (*Provider)(nil)
	_ openfeature.EventHandler             = (*Provider)(nil)
)

// New wraps an already-constructed *rollfuse.Client. The Client is not
// started here — Init/InitWithContext does that, called by OpenFeature's
// own openfeature.SetProvider(AndWait) — so construct the Client with
// rollfuse.NewClient exactly as you would to use it directly, then hand
// it to this constructor instead of calling client.Start/client.Evaluate
// yourself.
func New(client *rollfuse.Client) *Provider {
	return &Provider{
		client: client,
		events: make(chan openfeature.Event, eventChannelCapacity),
	}
}

// EventChannel implements openfeature.EventHandler: the channel
// openfeature.go-sdk's event executor drains to learn about this
// provider's lifecycle events, per task 10.1. ProviderReady/ProviderError
// need no wiring here — the SDK derives them itself from
// InitWithContext's own outcome (task 10.2's own doc comment explains
// why).
func (p *Provider) EventChannel() <-chan openfeature.Event {
	return p.events
}

// Metadata identifies this provider to OpenFeature.
func (p *Provider) Metadata() openfeature.Metadata {
	return openfeature.Metadata{Name: "rollfuse"}
}

// Hooks returns no provider-level hooks; register application hooks via
// the OpenFeature API/client directly instead.
func (p *Provider) Hooks() []openfeature.Hook {
	return nil
}

// Init starts the underlying rollfuse.Client (blocking until the first
// Configuration fetch succeeds, per go-sdk's own client.Start contract)
// with a 10-second timeout. Prefer InitWithContext (called instead by an
// OpenFeature SDK version that supports it) to control that timeout
// yourself.
func (p *Provider) Init(_ openfeature.EvaluationContext) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return p.InitWithContext(ctx, openfeature.EvaluationContext{})
}

// InitWithContext starts the underlying rollfuse.Client, blocking until
// the first Configuration fetch succeeds or ctx is done. Subscribes to
// the Client's own Configuration changes first, so a change that lands
// mid-Start (the first fetch itself) is never missed. Readiness is
// reported accurately (task 10.2) by this method's own outcome alone —
// returning nil makes openfeature.go-sdk fire ProviderReady, returning an
// error fires ProviderError — with no separate status this Provider needs
// to maintain: a FeatureProvider that doesn't implement CommonProvider's
// (deprecated) status field is, per the SDK's own doc comment, "assumed
// to be ready immediately," and the SDK derives its actual tracked state
// from exactly the events InitWithContext's outcome and trackConfigChange
// (below) produce.
func (p *Provider) InitWithContext(ctx context.Context, _ openfeature.EvaluationContext) error {
	p.unsubscribe = p.client.Subscribe(p.trackConfigChange)

	if err := p.client.Start(ctx); err != nil {
		return &openfeature.ProviderInitError{
			ErrorCode: openfeature.ProviderFatalCode,
			Message:   fmt.Sprintf("starting rollfuse client: %v", err),
		}
	}

	return nil
}

// trackConfigChange sends a ProviderConfigChange event for
// openfeature.go-sdk's event executor to relay to registered handlers
// (task 10.1), matching this package's own Metadata().Name. Non-blocking:
// see eventChannelCapacity's own doc comment for why a full channel drops
// this event rather than blocking the Client's background poll goroutine
// that calls it.
func (p *Provider) trackConfigChange() {
	event := openfeature.Event{
		ProviderName: p.Metadata().Name,
		EventType:    openfeature.ProviderConfigChange,
	}

	select {
	case p.events <- event:
	default:
	}
}

// Shutdown unsubscribes from the underlying rollfuse.Client and stops its
// background refresh and exposure-flush loops, with a 5-second timeout.
// Prefer ShutdownWithContext.
func (p *Provider) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = p.ShutdownWithContext(ctx)
}

// ShutdownWithContext unsubscribes from the underlying rollfuse.Client's
// Configuration changes and stops it. ctx is currently unused by go-sdk's
// own Close (it has no blocking network call to cancel), accepted only to
// satisfy openfeature.ContextAwareStateHandler.
func (p *Provider) ShutdownWithContext(_ context.Context) error {
	if p.unsubscribe != nil {
		p.unsubscribe()
	}

	return p.client.Close()
}

func (p *Provider) BooleanEvaluation(ctx context.Context, flag string, defaultValue bool, flatCtx openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	value, detail := resolve(ctx, p, flag, defaultValue, flatCtx)

	return openfeature.BoolResolutionDetail{Value: value, ProviderResolutionDetail: detail}
}

func (p *Provider) StringEvaluation(ctx context.Context, flag string, defaultValue string, flatCtx openfeature.FlattenedContext) openfeature.StringResolutionDetail {
	value, detail := resolve(ctx, p, flag, defaultValue, flatCtx)

	return openfeature.StringResolutionDetail{Value: value, ProviderResolutionDetail: detail}
}

func (p *Provider) FloatEvaluation(ctx context.Context, flag string, defaultValue float64, flatCtx openfeature.FlattenedContext) openfeature.FloatResolutionDetail {
	value, detail := resolve(ctx, p, flag, defaultValue, flatCtx)

	return openfeature.FloatResolutionDetail{Value: value, ProviderResolutionDetail: detail}
}

func (p *Provider) IntEvaluation(ctx context.Context, flag string, defaultValue int64, flatCtx openfeature.FlattenedContext) openfeature.IntResolutionDetail {
	value, detail := resolve(ctx, p, flag, defaultValue, flatCtx)

	return openfeature.IntResolutionDetail{Value: value, ProviderResolutionDetail: detail}
}

func (p *Provider) ObjectEvaluation(ctx context.Context, flag string, defaultValue any, flatCtx openfeature.FlattenedContext) openfeature.InterfaceResolutionDetail {
	value, detail := resolve(ctx, p, flag, defaultValue, flatCtx)

	return openfeature.InterfaceResolutionDetail{Value: value, ProviderResolutionDetail: detail}
}

// resolve is the one place that actually calls the wrapped
// rollfuse.Client, shared by every *Evaluation method above (Go doesn't
// allow a generic method on a non-generic receiver, so this is a
// package-level generic function taking the provider instead).
func resolve[T any](ctx context.Context, p *Provider, flag string, defaultValue T, flatCtx openfeature.FlattenedContext) (T, openfeature.ProviderResolutionDetail) {
	subjectKey, ok := flatCtx[openfeature.TargetingKey].(string)
	if !ok || subjectKey == "" {
		return defaultValue, openfeature.ProviderResolutionDetail{
			ResolutionError: openfeature.NewTargetingKeyMissingResolutionError(
				"rollfuse requires a non-empty targetingKey in the evaluation context (it becomes the flag's subject key)",
			),
			Reason: openfeature.ErrorReason,
		}
	}

	// Deliberately does not pass rollfuse.WithFallback: go-sdk treats a
	// supplied fallback as "never error, synthesize a default_fallback
	// result instead" for both an unready cache and an unknown flag key
	// (client.go's fallbackOrError) — which would mask ErrConfigNotReady
	// and ErrFlagNotFound entirely and make it impossible for this
	// provider to ever report OpenFeature's PROVIDER_NOT_READY or
	// FLAG_NOT_FOUND error codes, a real requirement of the OpenFeature
	// spec (every *Evaluation method already receives its own
	// defaultValue — this provider's job is to fall back to *that* one,
	// with the correct error code, not to ask go-sdk to silently
	// substitute its own).
	attrs, unrepresentableKeys := stringAttributes(flatCtx)

	var opts []rollfuse.EvaluateOption
	if len(attrs) > 0 {
		opts = append(opts, rollfuse.WithAttributes(attrs))
	}

	// task 10.3: an evaluation context value the attribute model can't
	// represent is reported here, on every ResolutionDetail this call
	// produces (including the error-path ones below), rather than
	// discarded silently — a diagnostic path a host can inspect
	// (ResolutionDetail.FlagMetadata) regardless of how the evaluation
	// itself turns out.
	var metadata openfeature.FlagMetadata
	if len(unrepresentableKeys) > 0 {
		metadata = openfeature.FlagMetadata{"unrepresentable_context_keys": unrepresentableKeys}
	}

	result, err := p.client.Evaluate(subjectKey, flag, opts...)
	if err != nil {
		return defaultValue, openfeature.ProviderResolutionDetail{
			ResolutionError: resolutionErrorFor(err),
			Reason:          openfeature.ErrorReason,
			FlagMetadata:    metadata,
		}
	}

	value, typeErr := decode[T](result.Value, defaultValue)
	if typeErr != nil {
		return defaultValue, openfeature.ProviderResolutionDetail{
			ResolutionError: openfeature.NewTypeMismatchResolutionError(
				fmt.Sprintf("flag %q's variation %q did not decode into the requested type: %v", flag, result.VariationKey, typeErr),
			),
			Reason:       openfeature.ErrorReason,
			Variant:      result.VariationKey,
			FlagMetadata: metadata,
		}
	}

	return value, openfeature.ProviderResolutionDetail{
		Reason:       reasonFor(result.Reason),
		Variant:      result.VariationKey,
		FlagMetadata: metadata,
	}
}

// decode unmarshals raw into T, except when T is `any` and defaultValue
// is untyped — ObjectEvaluation's contract is "whatever the flag's
// variation value is," so that path unmarshals into a plain
// map[string]any/[]any/etc. via the empty interface, same as
// encoding/json's default behavior for unknown-shaped JSON.
func decode[T any](raw json.RawMessage, defaultValue T) (T, error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return defaultValue, err
	}

	return value, nil
}

// resolutionErrorFor maps go-sdk's own sentinel errors to the matching
// OpenFeature ResolutionError code; anything else becomes GENERAL.
func resolutionErrorFor(err error) openfeature.ResolutionError {
	switch {
	case errors.Is(err, rollfuse.ErrConfigNotReady):
		return openfeature.NewProviderNotReadyResolutionError(err.Error())
	case errors.Is(err, rollfuse.ErrFlagNotFound):
		return openfeature.NewFlagNotFoundResolutionError(err.Error())
	default:
		return openfeature.NewGeneralResolutionError(err.Error(), err)
	}
}

// reasonFor maps go-sdk's EvaluationReason enum to OpenFeature's Reason
// enum. rollfuse's "default_fallback" (a matched rule whose outcome
// couldn't be resolved — should not happen for validly-constructed
// configuration) maps to DefaultReason rather than a new OpenFeature
// reason, since OpenFeature has no equivalent concept and the practical
// effect is the same: a default-ish value was served, not a targeted one.
func reasonFor(reason rollfuse.EvaluationReason) openfeature.Reason {
	switch reason {
	case rollfuse.ReasonRuleMatch:
		return openfeature.TargetingMatchReason
	case rollfuse.ReasonDefaultDisabled:
		return openfeature.DisabledReason
	case rollfuse.ReasonDefaultNoRuleMatch, rollfuse.ReasonDefaultFallback:
		return openfeature.DefaultReason
	default:
		return openfeature.UnknownReason
	}
}

// stringAttributes converts flatCtx into the map[string]string
// go-sdk's WithAttributes expects (rule matching is strict string
// equality — see go-sdk's own README), skipping targetingKey (already
// consumed as the subject key) and any value that isn't already a
// string. A non-string attribute (a number, bool, nested object) cannot
// be represented in rollfuse's attribute model — this provider does not
// silently stringify it with fmt.Sprintf, which would let e.g. attribute
// values `"true"` (string) and `true` (bool) match a condition meant for
// only one of them — so it never matches a rule condition, the same as
// an attribute omitted entirely, but its key is returned separately
// (task 10.3's diagnostic path — see resolve's own use of it) rather
// than discarded with no trace.
func stringAttributes(flatCtx openfeature.FlattenedContext) (attrs map[string]string, unrepresentableKeys []string) {
	attrs = make(map[string]string, len(flatCtx))

	for k, v := range flatCtx {
		if k == openfeature.TargetingKey {
			continue
		}

		if s, ok := v.(string); ok {
			attrs[k] = s

			continue
		}

		unrepresentableKeys = append(unrepresentableKeys, k)
	}

	return attrs, unrepresentableKeys
}
