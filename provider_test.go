package rollfuseprovider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	rollfuse "github.com/rollfuse/go-sdk"
	rollfuseprovider "github.com/rollfuse/openfeature-provider"
)

func testConfigurationServer(t *testing.T, flags ...rollfuse.FlagConfig) *httptest.Server {
	t.Helper()

	cfg := rollfuse.Configuration{EnvironmentID: "env_test", Version: 1, Flags: flags}

	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshaling test configuration: %v", err)
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/config":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case "/v1/exposure-events":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accepted":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newReadyProvider(t *testing.T, flags ...rollfuse.FlagConfig) (*rollfuseprovider.Provider, func()) {
	t.Helper()

	server := testConfigurationServer(t, flags...)

	client, err := rollfuse.NewClient(server.URL, "test-credential")
	if err != nil {
		t.Fatalf("rollfuse.NewClient: %v", err)
	}

	provider := rollfuseprovider.New(client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := provider.InitWithContext(ctx, openfeature.EvaluationContext{}); err != nil {
		t.Fatalf("InitWithContext: %v", err)
	}

	return provider, func() {
		_ = provider.ShutdownWithContext(context.Background())

		server.Close()
	}
}

func booleanFlag() rollfuse.FlagConfig {
	return rollfuse.FlagConfig{
		FlagKey:          "checkout-redesign",
		Enabled:          true,
		DefaultVariation: "off",
		Variations: []rollfuse.Variation{
			{Key: "on", Value: json.RawMessage(`true`)},
			{Key: "off", Value: json.RawMessage(`false`)},
		},
		Rules: []rollfuse.Rule{
			{
				Conditions: []rollfuse.Condition{{Attribute: "plan", Value: "enterprise"}},
				Outcome:    rollfuse.Outcome{VariationKey: "on"},
			},
		},
	}
}

func stringFlag() rollfuse.FlagConfig {
	return rollfuse.FlagConfig{
		FlagKey:          "welcome-message",
		Enabled:          true,
		DefaultVariation: "classic",
		Variations: []rollfuse.Variation{
			{Key: "classic", Value: json.RawMessage(`"Welcome!"`)},
			{Key: "friendly", Value: json.RawMessage(`"Hey there!"`)},
		},
		Rules: nil,
	}
}

func numberFlag() rollfuse.FlagConfig {
	return rollfuse.FlagConfig{
		FlagKey:          "max-items",
		Enabled:          true,
		DefaultVariation: "small",
		Variations: []rollfuse.Variation{
			{Key: "small", Value: json.RawMessage(`10`)},
			{Key: "large", Value: json.RawMessage(`100`)},
		},
		Rules: nil,
	}
}

func objectFlag() rollfuse.FlagConfig {
	return rollfuse.FlagConfig{
		FlagKey:          "checkout-config",
		Enabled:          true,
		DefaultVariation: "default",
		Variations: []rollfuse.Variation{
			{Key: "default", Value: json.RawMessage(`{"maxRetries":3,"timeoutMs":500}`)},
		},
		Rules: nil,
	}
}

func TestProvider_Metadata(t *testing.T) {
	provider := rollfuseprovider.New(nil) //nolint:staticcheck // Metadata never touches the client

	if got := provider.Metadata().Name; got != "rollfuse" {
		t.Fatalf("expected metadata name %q, got %q", "rollfuse", got)
	}
}

func TestProvider_BooleanEvaluation_RuleMatch(t *testing.T) {
	provider, cleanup := newReadyProvider(t, booleanFlag())
	defer cleanup()

	detail := provider.BooleanEvaluation(context.Background(), "checkout-redesign", false, openfeature.FlattenedContext{
		openfeature.TargetingKey: "user_1",
		"plan":                   "enterprise",
	})

	if detail.Value != true {
		t.Fatalf("expected true, got %v", detail.Value)
	}

	if detail.Variant != "on" {
		t.Fatalf("expected variant %q, got %q", "on", detail.Variant)
	}

	if detail.Reason != openfeature.TargetingMatchReason {
		t.Fatalf("expected reason %q, got %q", openfeature.TargetingMatchReason, detail.Reason)
	}
}

func TestProvider_BooleanEvaluation_DefaultNoRuleMatch(t *testing.T) {
	provider, cleanup := newReadyProvider(t, booleanFlag())
	defer cleanup()

	detail := provider.BooleanEvaluation(context.Background(), "checkout-redesign", true, openfeature.FlattenedContext{
		openfeature.TargetingKey: "user_1",
		"plan":                   "free",
	})

	if detail.Value != false {
		t.Fatalf("expected false (the flag's default_variation), got %v", detail.Value)
	}

	if detail.Reason != openfeature.DefaultReason {
		t.Fatalf("expected reason %q, got %q", openfeature.DefaultReason, detail.Reason)
	}
}

func TestProvider_StringEvaluation(t *testing.T) {
	provider, cleanup := newReadyProvider(t, stringFlag())
	defer cleanup()

	detail := provider.StringEvaluation(context.Background(), "welcome-message", "fallback", openfeature.FlattenedContext{
		openfeature.TargetingKey: "user_1",
	})

	if detail.Value != "Welcome!" {
		t.Fatalf("expected %q, got %q", "Welcome!", detail.Value)
	}
}

func TestProvider_IntEvaluation(t *testing.T) {
	provider, cleanup := newReadyProvider(t, numberFlag())
	defer cleanup()

	detail := provider.IntEvaluation(context.Background(), "max-items", -1, openfeature.FlattenedContext{
		openfeature.TargetingKey: "user_1",
	})

	if detail.Value != 10 {
		t.Fatalf("expected 10, got %v", detail.Value)
	}
}

func TestProvider_FloatEvaluation(t *testing.T) {
	provider, cleanup := newReadyProvider(t, numberFlag())
	defer cleanup()

	detail := provider.FloatEvaluation(context.Background(), "max-items", -1, openfeature.FlattenedContext{
		openfeature.TargetingKey: "user_1",
	})

	if detail.Value != 10.0 {
		t.Fatalf("expected 10.0, got %v", detail.Value)
	}
}

func TestProvider_ObjectEvaluation(t *testing.T) {
	provider, cleanup := newReadyProvider(t, objectFlag())
	defer cleanup()

	detail := provider.ObjectEvaluation(context.Background(), "checkout-config", nil, openfeature.FlattenedContext{
		openfeature.TargetingKey: "user_1",
	})

	m, ok := detail.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", detail.Value)
	}

	if m["timeoutMs"] != 500.0 {
		t.Fatalf("expected timeoutMs=500, got %v", m["timeoutMs"])
	}
}

func TestProvider_MissingTargetingKey(t *testing.T) {
	provider, cleanup := newReadyProvider(t, booleanFlag())
	defer cleanup()

	detail := provider.BooleanEvaluation(context.Background(), "checkout-redesign", false, openfeature.FlattenedContext{})

	if detail.Value != false {
		t.Fatalf("expected the default value back, got %v", detail.Value)
	}

	if err := detail.Error(); err == nil {
		t.Fatal("expected a resolution error")
	}

	resErr := detail.ResolutionError
	if got := detail.Reason; got != openfeature.ErrorReason {
		t.Fatalf("expected reason %q, got %q", openfeature.ErrorReason, got)
	}

	if !isTargetingKeyMissing(resErr) {
		t.Fatalf("expected TARGETING_KEY_MISSING, got: %v", resErr)
	}
}

func TestProvider_FlagNotFound(t *testing.T) {
	provider, cleanup := newReadyProvider(t, booleanFlag())
	defer cleanup()

	detail := provider.BooleanEvaluation(context.Background(), "does-not-exist", true, openfeature.FlattenedContext{
		openfeature.TargetingKey: "user_1",
	})

	if detail.Value != true {
		t.Fatalf("expected the default value back, got %v", detail.Value)
	}

	if detail.Reason != openfeature.ErrorReason {
		t.Fatalf("expected reason %q, got %q", openfeature.ErrorReason, detail.Reason)
	}
}

func TestProvider_TypeMismatch(t *testing.T) {
	provider, cleanup := newReadyProvider(t, stringFlag())
	defer cleanup()

	// "welcome-message" resolves to a string variation; asking for a bool
	// must fail to decode, not silently succeed with a zero value.
	detail := provider.BooleanEvaluation(context.Background(), "welcome-message", true, openfeature.FlattenedContext{
		openfeature.TargetingKey: "user_1",
	})

	if detail.Value != true {
		t.Fatalf("expected the default value back on type mismatch, got %v", detail.Value)
	}

	if detail.Reason != openfeature.ErrorReason {
		t.Fatalf("expected reason %q, got %q", openfeature.ErrorReason, detail.Reason)
	}
}

func TestProvider_NonStringAttributesAreExcludedNotStringified(t *testing.T) {
	provider, cleanup := newReadyProvider(t, booleanFlag())
	defer cleanup()

	// "plan" here is a bool, not the string "enterprise" the rule
	// requires — must not match via fmt.Sprintf-style coercion.
	detail := provider.BooleanEvaluation(context.Background(), "checkout-redesign", false, openfeature.FlattenedContext{
		openfeature.TargetingKey: "user_1",
		"plan":                   true,
	})

	if detail.Reason != openfeature.DefaultReason {
		t.Fatalf("expected the non-string attribute to be excluded (default reason), got reason %q value %v", detail.Reason, detail.Value)
	}
}

// TestProvider_UnrepresentableAttributeReportedThroughDiagnosticPath
// exercises harden-sdk-runtime task 10.3: a context value the attribute
// model can't represent is reported through FlagMetadata rather than
// discarded silently. Manually verified: removing the
// unrepresentableKeys plumbing in resolve made this test's
// FlagMetadata.GetStringSlice call return an empty result instead of
// ["plan"]; restored before committing.
func TestProvider_UnrepresentableAttributeReportedThroughDiagnosticPath(t *testing.T) {
	provider, cleanup := newReadyProvider(t, booleanFlag())
	defer cleanup()

	detail := provider.BooleanEvaluation(context.Background(), "checkout-redesign", false, openfeature.FlattenedContext{
		openfeature.TargetingKey: "user_1",
		"plan":                   true,
	})

	if detail.FlagMetadata == nil {
		t.Fatal("expected FlagMetadata to report the unrepresentable attribute, got nil")
	}

	keys, ok := detail.FlagMetadata["unrepresentable_context_keys"].([]string)
	if !ok {
		t.Fatalf("expected unrepresentable_context_keys to be a []string, got %#v", detail.FlagMetadata["unrepresentable_context_keys"])
	}

	if len(keys) != 1 || keys[0] != "plan" {
		t.Fatalf(`expected unrepresentable_context_keys to be ["plan"], got %v`, keys)
	}
}

// TestProvider_EmitsProviderConfigChange exercises task 10.1: the
// Provider emits ProviderConfigChange when the wrapped Client's
// Configuration changes. Manually verified: removing the
// client.Subscribe wiring in InitWithContext made this test's channel
// read time out instead of receiving an event; restored before
// committing.
func TestProvider_EmitsProviderConfigChange(t *testing.T) {
	var requestCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/config" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		requestCount++

		flag := booleanFlag()
		if requestCount >= 2 {
			flag.Rules = nil
		}

		cfg := rollfuse.Configuration{EnvironmentID: "env_test", Version: int64(requestCount), Flags: []rollfuse.FlagConfig{flag}}
		body, _ := json.Marshal(cfg)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client, err := rollfuse.NewClient(server.URL, "test-credential", rollfuse.WithRefreshInterval(20*time.Millisecond))
	if err != nil {
		t.Fatalf("rollfuse.NewClient: %v", err)
	}

	provider := rollfuseprovider.New(client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := provider.InitWithContext(ctx, openfeature.EvaluationContext{}); err != nil {
		t.Fatalf("InitWithContext: %v", err)
	}
	defer func() { _ = provider.ShutdownWithContext(context.Background()) }()

	select {
	case event := <-provider.EventChannel():
		if event.EventType != openfeature.ProviderConfigChange {
			t.Fatalf("expected a ProviderConfigChange event, got %q", event.EventType)
		}

		if event.ProviderName != provider.Metadata().Name {
			t.Fatalf("expected ProviderName %q, got %q", provider.Metadata().Name, event.ProviderName)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a ProviderConfigChange event within the deadline")
	}
}

// TestProvider_ReportsReadyStateAccurately exercises task 10.2: a host
// querying the provider's state (here, via a fresh InitWithContext call's
// own success/failure, which is what openfeature.go-sdk's own state
// tracking is driven by) gets an accurate answer in both directions.
func TestProvider_ReportsReadyStateAccurately(t *testing.T) {
	t.Run("succeeds when the first Configuration fetch succeeds", func(t *testing.T) {
		provider, cleanup := newReadyProvider(t, booleanFlag())
		defer cleanup()

		// newReadyProvider already asserts InitWithContext returned nil;
		// confirm evaluation actually works, proving the client is truly
		// usable, not just that Init happened to return without error.
		detail := provider.BooleanEvaluation(context.Background(), "checkout-redesign", false, openfeature.FlattenedContext{
			openfeature.TargetingKey: "user_1",
			"plan":                   "enterprise",
		})

		if detail.Value != true {
			t.Fatalf("expected a ready provider to serve a real evaluation, got %+v", detail)
		}
	})

	t.Run("fails when the underlying fetch never succeeds", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client, err := rollfuse.NewClient(server.URL, "test-credential")
		if err != nil {
			t.Fatalf("rollfuse.NewClient: %v", err)
		}
		defer func() { _ = client.Close() }()

		provider := rollfuseprovider.New(client)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		if err := provider.InitWithContext(ctx, openfeature.EvaluationContext{}); err == nil {
			t.Fatal("expected InitWithContext to return an error when the platform is unreachable")
		}
	})
}

// isTargetingKeyMissing checks for the TARGETING_KEY_MISSING code.
// openfeature.ResolutionError doesn't expose its code publicly, so this
// compares its formatted error string instead — the SDK guarantees the
// code appears verbatim at the start (see ResolutionError.Error in
// openfeature/resolution_error.go).
func isTargetingKeyMissing(err error) bool {
	prefix := string(openfeature.TargetingKeyMissingCode)

	return err != nil && len(err.Error()) >= len(prefix) && err.Error()[:len(prefix)] == prefix
}
