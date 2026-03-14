package model

type Outcome string

const (
	OutcomeUp   Outcome = "Up"
	OutcomeDown Outcome = "Down"
)

type TradeSide string

const (
	TradeSideBuy  TradeSide = "BUY"
	TradeSideSell TradeSide = "SELL"
)

type Order struct {
	ID              string
	Status          string
	Error           bool
	ErrorMsg        string
	ConditionID     string
	OriginalSize    string
	MatchedSize     string
	Price           string
	TokenID         string
	Outcome         Outcome
	TradeSide       TradeSide
	AssociateTrades string
}
