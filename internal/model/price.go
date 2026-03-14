package model

import "github.com/shopspring/decimal"

type Price struct {
	UpAsk   decimal.Decimal
	UpBid   decimal.Decimal
	DownAsk decimal.Decimal
	DownBid decimal.Decimal
}
