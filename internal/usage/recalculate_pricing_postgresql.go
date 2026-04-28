package usage

import (
	"context"
	"fmt"
)

// RecalculatePricing updates matching PostgreSQL usage rows with costs computed
// from the supplied pricing resolver.
func (s *PostgreSQLStore) RecalculatePricing(ctx context.Context, params RecalculatePricingParams, resolver PricingResolver) (RecalculatePricingResult, error) {
	if err := recalculatePricingUnavailable(resolver); err != nil {
		return RecalculatePricingResult{}, err
	}
	params = normalizedRecalculatePricingParams(params)

	entries, err := s.postgresRecalculationEntries(ctx, params)
	if err != nil {
		return RecalculatePricingResult{}, err
	}
	if len(entries) == 0 {
		return finalizeRecalculatePricingResult(RecalculatePricingResult{}), nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RecalculatePricingResult{}, fmt.Errorf("begin postgres pricing recalculation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := RecalculatePricingResult{}
	for _, entry := range entries {
		update := recalculateEntryCosts(entry, resolver)
		if _, err := tx.Exec(ctx, `
			UPDATE usage
			SET input_cost = $1, output_cost = $2, total_cost = $3, costs_calculation_caveat = $4
			WHERE id = $5::uuid
		`,
			nullableFloat(update.InputCost),
			nullableFloat(update.OutputCost),
			nullableFloat(update.TotalCost),
			update.Caveat,
			update.ID,
		); err != nil {
			return RecalculatePricingResult{}, fmt.Errorf("update postgres usage cost %s: %w", update.ID, err)
		}
		updateRecalculatePricingResult(&result, update)
	}

	if err := tx.Commit(ctx); err != nil {
		return RecalculatePricingResult{}, fmt.Errorf("commit postgres pricing recalculation: %w", err)
	}
	return finalizeRecalculatePricingResult(result), nil
}

func (s *PostgreSQLStore) postgresRecalculationEntries(ctx context.Context, params RecalculatePricingParams) ([]recalculationEntry, error) {
	conditions, args, nextIdx, err := pgUsageConditions(params.UsageQueryParams, 1)
	if err != nil {
		return nil, err
	}
	if params.Model != "" {
		conditions = append(conditions, fmt.Sprintf("model = $%d", nextIdx))
		args = append(args, params.Model)
		nextIdx++
	}
	if params.Provider != "" {
		conditions = append(conditions, fmt.Sprintf("(provider = $%d OR provider_name = $%d)", nextIdx, nextIdx+1))
		args = append(args, params.Provider, params.Provider)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id::text, model, provider, endpoint, input_tokens, output_tokens, raw_data::text
		FROM usage`+buildWhereClause(conditions), args...)
	if err != nil {
		return nil, fmt.Errorf("query postgres usage costs for recalculation: %w", err)
	}
	defer rows.Close()

	entries := make([]recalculationEntry, 0)
	for rows.Next() {
		var entry recalculationEntry
		var rawData *string
		if err := rows.Scan(
			&entry.ID,
			&entry.Model,
			&entry.Provider,
			&entry.Endpoint,
			&entry.InputTokens,
			&entry.OutputTokens,
			&rawData,
		); err != nil {
			return nil, fmt.Errorf("scan postgres usage cost row: %w", err)
		}
		if rawData != nil {
			entry.RawData = rawDataFromJSON(*rawData, entry.ID)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres usage costs for recalculation: %w", err)
	}
	return entries, nil
}
