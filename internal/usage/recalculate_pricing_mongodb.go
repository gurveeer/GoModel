package usage

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// RecalculatePricing updates matching MongoDB usage documents with costs
// computed from the supplied pricing resolver.
func (s *MongoDBStore) RecalculatePricing(ctx context.Context, params RecalculatePricingParams, resolver PricingResolver) (RecalculatePricingResult, error) {
	if err := recalculatePricingUnavailable(resolver); err != nil {
		return RecalculatePricingResult{}, err
	}
	params = normalizedRecalculatePricingParams(params)

	filter, err := mongoRecalculationFilter(params)
	if err != nil {
		return RecalculatePricingResult{}, err
	}

	cursor, err := s.collection.Find(ctx, filter)
	if err != nil {
		return RecalculatePricingResult{}, fmt.Errorf("query mongodb usage costs for recalculation: %w", err)
	}
	defer cursor.Close(ctx)

	result := RecalculatePricingResult{}
	for cursor.Next(ctx) {
		var row struct {
			ID           string         `bson:"_id"`
			Model        string         `bson:"model"`
			Provider     string         `bson:"provider"`
			Endpoint     string         `bson:"endpoint"`
			InputTokens  int            `bson:"input_tokens"`
			OutputTokens int            `bson:"output_tokens"`
			RawData      map[string]any `bson:"raw_data"`
		}
		if err := cursor.Decode(&row); err != nil {
			return RecalculatePricingResult{}, fmt.Errorf("scan mongodb usage cost row: %w", err)
		}

		update := recalculateEntryCosts(recalculationEntry{
			ID:           row.ID,
			Model:        row.Model,
			Provider:     row.Provider,
			Endpoint:     row.Endpoint,
			InputTokens:  row.InputTokens,
			OutputTokens: row.OutputTokens,
			RawData:      row.RawData,
		}, resolver)

		if _, err := s.collection.UpdateByID(ctx, update.ID, mongoRecalculationUpdate(update)); err != nil {
			return RecalculatePricingResult{}, fmt.Errorf("update mongodb usage cost %s: %w", update.ID, err)
		}
		updateRecalculatePricingResult(&result, update)
	}
	if err := cursor.Err(); err != nil {
		return RecalculatePricingResult{}, fmt.Errorf("iterate mongodb usage costs for recalculation: %w", err)
	}
	return finalizeRecalculatePricingResult(result), nil
}

func mongoRecalculationFilter(params RecalculatePricingParams) (bson.D, error) {
	filter, err := mongoUsageMatchFilters(params.UsageQueryParams)
	if err != nil {
		return nil, err
	}
	if params.Model != "" {
		filter = append(filter, bson.E{Key: "model", Value: params.Model})
	}
	if params.Provider != "" {
		filter = mongoAndFilters(filter, bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "provider", Value: params.Provider}},
			bson.D{{Key: "provider_name", Value: params.Provider}},
		}}})
	}
	return filter, nil
}

func mongoRecalculationUpdate(update recalculationUpdate) bson.D {
	set := bson.D{{Key: "costs_calculation_caveat", Value: update.Caveat}}
	unset := bson.D{}

	if update.InputCost != nil {
		set = append(set, bson.E{Key: "input_cost", Value: *update.InputCost})
	} else {
		unset = append(unset, bson.E{Key: "input_cost", Value: ""})
	}
	if update.OutputCost != nil {
		set = append(set, bson.E{Key: "output_cost", Value: *update.OutputCost})
	} else {
		unset = append(unset, bson.E{Key: "output_cost", Value: ""})
	}
	if update.TotalCost != nil {
		set = append(set, bson.E{Key: "total_cost", Value: *update.TotalCost})
	} else {
		unset = append(unset, bson.E{Key: "total_cost", Value: ""})
	}

	result := bson.D{{Key: "$set", Value: set}}
	if len(unset) > 0 {
		result = append(result, bson.E{Key: "$unset", Value: unset})
	}
	return result
}
