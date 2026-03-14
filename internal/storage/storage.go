package storage

import (
	"database/sql"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/model"
)

type Storage interface {
	TxWrap(fn func(tx *sql.Tx) error) error

	GetMarket(marketID string) (model.Market, error)
	GetResolvedMarkets() ([]model.Market, error)
	GetPlannedAndActiveMarkets() ([]string, error)
	InsertMarket(tx *sql.Tx, market model.Market) error
	UpdateMarketStatus(tx *sql.Tx, marketID string, status model.MarketStatus) error

	InsertOrder(tx *sql.Tx, order model.Order) error
	GetOrdersByCondition(conditionID string) ([]model.Order, error)

	InsertStats(tx *sql.Tx, stats model.Stats) error
}
