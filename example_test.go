package rollfuseprovider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/open-feature/go-sdk/openfeature"
	rollfuse "github.com/rollfuse/go-sdk"
	rollfuseprovider "github.com/rollfuse/openfeature-provider"
)

// Example_openFeatureClient exercises this provider through the real,
// public github.com/open-feature/go-sdk API — openfeature.SetProviderAndWait
// and openfeature.NewDefaultClient — not just the Provider struct
// directly, so it proves the whole integration works the way a consuming
// application would actually use it.
func Example_openFeatureClient() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/config":
			cfg := rollfuse.Configuration{
				EnvironmentID: "env_demo",
				Version:       1,
				Flags: []rollfuse.FlagConfig{
					{
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
					},
				},
			}
			body, _ := json.Marshal(cfg)

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case "/v1/exposure-events":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accepted":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := rollfuse.NewClient(server.URL, "demo-credential")
	if err != nil {
		panic(err)
	}

	if err := openfeature.SetProviderAndWait(rollfuseprovider.New(client)); err != nil {
		panic(err)
	}

	defer openfeature.Shutdown()

	ofClient := openfeature.NewDefaultClient()

	enabled, err := ofClient.BooleanValue(
		context.Background(),
		"checkout-redesign",
		false,
		openfeature.NewEvaluationContext("user_1", map[string]any{"plan": "enterprise"}),
	)
	if err != nil {
		panic(err)
	}

	fmt.Println("checkout-redesign for an enterprise user:", enabled)

	// Output: checkout-redesign for an enterprise user: true
}
