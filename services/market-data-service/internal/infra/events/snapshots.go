package events

import domain "marketmind/services/market-data-service/internal/domain/market"

func newFilings() *domain.Snapshot           { return &domain.Snapshot{} }
func newEconomics() *domain.EconomicSnapshot { return &domain.EconomicSnapshot{} }
func newPrices() *domain.PriceSnapshot       { return &domain.PriceSnapshot{} }
