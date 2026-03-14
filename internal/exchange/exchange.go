package exchange

import (
	"context"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/model"
)

type Client interface {
	GetMarkets(ctx context.Context) ([]model.Market, error)
	GetPrices(tokenUp, tokenDown string) (model.Price, error)
	PlaceOrder(tokenID string, outcome model.Outcome, price, size string) (model.Order, error)
	GetOrder(ctx context.Context, orderHash string) (model.Order, error)
	Redeem(ctx context.Context, conditionID string) (int64, error)
}
