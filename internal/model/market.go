package model

import "time"

type MarketStatus string

const (
	MarketStatusPlanned  MarketStatus = "planned"
	MarketStatusActive   MarketStatus = "active"
	MarketStatusResolved MarketStatus = "resolved"
	MarketStatusRedeemed MarketStatus = "redeemed"
)

type UnderlyingType string

const (
	UnderlyingBTC UnderlyingType = "BTC"
	UnderlyingETH UnderlyingType = "ETH"
	UnderlyingSOL UnderlyingType = "SOL"
	UnderlyingXRP UnderlyingType = "XRP"
)

type Market struct {
	ID          string
	EventID     string
	Question    string
	ConditionID string
	Slug        string
	StartTime   time.Time
	EndTime     time.Time
	TokenUp     string
	TokenDown   string
	Status      MarketStatus
	Underlying  UnderlyingType
}

func (m *Market) IsExpired() bool {
	return time.Now().After(m.EndTime)
}
