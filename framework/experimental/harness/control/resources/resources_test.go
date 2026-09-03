package resources

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// partialModelsProvider returns a half-built model catalog together
// with an error (the shape a transport produces when the listing call
// fails mid-page). The catalog is display-only: it must degrade to an
// EMPTY model list, not present models the provider said it could not
// vouch for.
type partialModelsProvider struct{}

func (partialModelsProvider) Name() string { return "partial" }

func (partialModelsProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	return nil, errors.New("not implemented in test stub")
}

func (partialModelsProvider) Models(_ context.Context) ([]provider.Model, error) {
	return []provider.Model{{ID: "half-listed"}}, errors.New("listing failed mid-page")
}

func (partialModelsProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

func TestListProvidersClearsPartialModelsOnError(t *testing.T) {
	cat := NewCatalog()
	cat.Providers = append(cat.Providers, partialModelsProvider{})

	out := cat.ListProviders(context.Background())
	if len(out) != 1 {
		t.Fatalf("providers = %d, want 1", len(out))
	}
	if out[0].Name != "partial" {
		t.Fatalf("name = %q", out[0].Name)
	}
	if len(out[0].Models) != 0 {
		t.Fatalf("partial model list shipped with the error: %d models (first = %q); want empty",
			len(out[0].Models), out[0].Models[0].ID)
	}
}
