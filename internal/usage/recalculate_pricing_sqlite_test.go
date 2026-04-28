package usage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"gomodel/internal/core"
)

type staticTestPricingResolver map[string]*core.ModelPricing

func (r staticTestPricingResolver) ResolvePricing(model, providerType string) *core.ModelPricing {
	return r[providerType+"/"+model]
}

func TestSQLiteStoreRecalculatePricingUpdatesFilteredUsageCosts(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	store, err := NewSQLiteStore(db, 0)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}

	oldCost := 99.0
	ctx := context.Background()
	if err := store.WriteBatch(ctx, []*UsageEntry{
		{
			ID:           "usage-match",
			RequestID:    "req-match",
			ProviderID:   "provider-match",
			Timestamp:    time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
			Model:        "gpt-4o",
			Provider:     "openai",
			ProviderName: "primary-openai",
			Endpoint:     "/v1/chat/completions",
			UserPath:     "/team/alpha",
			InputTokens:  1_000_000,
			OutputTokens: 500_000,
			TotalTokens:  1_500_000,
			InputCost:    &oldCost,
			OutputCost:   &oldCost,
			TotalCost:    &oldCost,
		},
		{
			ID:          "usage-other-model",
			RequestID:   "req-other",
			ProviderID:  "provider-other",
			Timestamp:   time.Date(2026, 4, 12, 11, 0, 0, 0, time.UTC),
			Model:       "gpt-4o-mini",
			Provider:    "openai",
			Endpoint:    "/v1/chat/completions",
			UserPath:    "/team/alpha",
			InputTokens: 1_000_000,
			TotalTokens: 1_000_000,
			TotalCost:   &oldCost,
		},
	}); err != nil {
		t.Fatalf("WriteBatch() error = %v", err)
	}

	inputRate := 2.0
	outputRate := 6.0
	result, err := store.RecalculatePricing(ctx, RecalculatePricingParams{
		UsageQueryParams: UsageQueryParams{
			StartDate: time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
			EndDate:   time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
			UserPath:  "/team",
		},
		Provider: "primary-openai",
		Model:    "gpt-4o",
	}, staticTestPricingResolver{
		"primary-openai/gpt-4o": {
			InputPerMtok:  &inputRate,
			OutputPerMtok: &outputRate,
		},
	})
	if err != nil {
		t.Fatalf("RecalculatePricing() error = %v", err)
	}
	if result.Matched != 1 || result.Recalculated != 1 || result.WithPricing != 1 || result.WithoutPricing != 0 {
		t.Fatalf("result = %+v, want one recalculated row with pricing", result)
	}

	var inputCost, outputCost, totalCost float64
	if err := db.QueryRow(`SELECT input_cost, output_cost, total_cost FROM usage WHERE id = 'usage-match'`).Scan(&inputCost, &outputCost, &totalCost); err != nil {
		t.Fatalf("query recalculated row: %v", err)
	}
	if inputCost != 2.0 || outputCost != 3.0 || totalCost != 5.0 {
		t.Fatalf("costs = input %.4f output %.4f total %.4f, want 2/3/5", inputCost, outputCost, totalCost)
	}

	var otherTotal float64
	if err := db.QueryRow(`SELECT total_cost FROM usage WHERE id = 'usage-other-model'`).Scan(&otherTotal); err != nil {
		t.Fatalf("query untouched row: %v", err)
	}
	if otherTotal != oldCost {
		t.Fatalf("other total cost = %.4f, want %.4f", otherTotal, oldCost)
	}
}
