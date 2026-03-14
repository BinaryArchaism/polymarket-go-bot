package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"github.com/rs/zerolog"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/config"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/exchange"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/model"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/storage"
)

// marketState holds the subset of model.Market fields needed by the worker.
type marketState struct {
	id          string
	conditionID string
	tokenUp     string
	tokenDown   string
	startTime   time.Time
	endTime     time.Time
}

// marketWorker manages the lifecycle and trading for a single market.
type marketWorker struct {
	market marketState

	// price fetching
	askUp   decimal.Decimal
	bidUp   decimal.Decimal
	askDown decimal.Decimal
	bidDown decimal.Decimal

	// position state
	pFairVal     decimal.Decimal
	qUp          decimal.Decimal
	pUp          decimal.Decimal
	qDown        decimal.Decimal
	pDown        decimal.Decimal
	tte          decimal.Decimal
	riskVal      decimal.Decimal
	moneyTiltVal decimal.Decimal
	targetTiltVal decimal.Decimal
	lastDecision time.Time

	// config
	marketTimeMs       decimal.Decimal
	riskLimit          decimal.Decimal
	minSize            decimal.Decimal
	maxStepRisk        decimal.Decimal
	kMin               decimal.Decimal
	kMax               decimal.Decimal
	alpha              decimal.Decimal
	tauMin             decimal.Decimal
	tauMax             decimal.Decimal
	epsMin             decimal.Decimal
	epsMax             decimal.Decimal
	eMax               decimal.Decimal
	stopTradeTTEMs     decimal.Decimal
	decisionDebounceMs decimal.Decimal

	// dependencies
	client  exchange.Client
	storage storage.Storage
	log     zerolog.Logger

	closed    chan struct{}
	closeOnce sync.Once
}

func newMarketWorker(
	cfg config.Config,
	cli exchange.Client,
	store storage.Storage,
	logger zerolog.Logger,
	marketID string,
) *marketWorker {
	// Market time = EndTime - StartTime in ms (set when market data is loaded)
	return &marketWorker{
		market:   marketState{id: marketID},
		client:   cli,
		storage:  store,
		log:      logger.With().Str("component", "market_worker").Str("market_id", marketID).Logger(),
		closed:   make(chan struct{}),

		// Strategy config from config
		riskLimit:          cfg.Strategy.RiskLimit,
		minSize:            cfg.Strategy.MinSize,
		maxStepRisk:        cfg.Strategy.MaxStep,
		kMin:               cfg.Strategy.KMin,
		kMax:               cfg.Strategy.KMax,
		alpha:              cfg.Strategy.Alpha,
		tauMin:             cfg.Strategy.TauMin,
		tauMax:             cfg.Strategy.TauMax,
		epsMin:             cfg.Strategy.EpsMin,
		epsMax:             cfg.Strategy.EpsMax,
		eMax:               cfg.Strategy.EMax,
		stopTradeTTEMs:     decimal.NewFromInt(cfg.Strategy.StopTTE),
		decisionDebounceMs: decimal.NewFromInt(cfg.Strategy.Debounce),
	}
}

func (mw *marketWorker) Run(ctx context.Context, pollInterval time.Duration) error {
	m, err := mw.storage.GetMarket(mw.market.id)
	if err != nil {
		return fmt.Errorf("get market from storage: %w", err)
	}
	mw.market = marketState{
		id:          m.ID,
		conditionID: m.ConditionID,
		tokenUp:     m.TokenUp,
		tokenDown:   m.TokenDown,
		startTime:   m.StartTime,
		endTime:     m.EndTime,
	}

	// Calculate market time in milliseconds
	mw.marketTimeMs = decimal.NewFromInt(m.EndTime.Sub(m.StartTime).Milliseconds())

	// Wait for market to become active
	sleepTime := time.Until(m.StartTime)
	if sleepTime > 0 {
		mw.log.Debug().Str("sleep", sleepTime.String()).Msg("waiting for market opening")
		t := time.NewTimer(sleepTime)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}

	mw.storage.UpdateMarketStatus(nil, mw.market.id, model.MarketStatusActive)
	mw.log.Info().Msg("market active, starting trading loop")

	mw.tradingLoop(ctx, pollInterval)
	return nil
}

func (mw *marketWorker) tradingLoop(ctx context.Context, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	mw.processTick()

	for {
		select {
		case <-mw.closed:
			mw.log.Info().Msg("market closed")
			return
		case <-ctx.Done():
			mw.log.Info().Msg("received close signal")
			mw.close()
			return
		case <-ticker.C:
			mw.processTick()
		}
	}
}

func (mw *marketWorker) processTick() {
	if time.Now().After(mw.market.endTime) {
		mw.log.Info().Time("end_time", mw.market.endTime).Msg("market expired")
		mw.storage.UpdateMarketStatus(nil, mw.market.id, model.MarketStatusResolved)
		mw.close()
		return
	}

	// Fetch prices
	prices, err := mw.client.GetPrices(mw.market.tokenUp, mw.market.tokenDown)
	if err != nil {
		mw.log.Error().Err(err).Msg("failed to get prices")
		return
	}

	mw.askUp = prices.UpAsk
	mw.bidUp = prices.UpBid
	mw.askDown = prices.DownAsk
	mw.bidDown = prices.DownBid

	// Run v2 trading algorithm
	shouldTrade, order := mw.processNewMarketState()
	if !shouldTrade {
		return
	}

	// Determine which token to buy
	midUp := mw.askUp.Add(mw.bidUp).Div(decTwo)
	midDown := mw.askDown.Add(mw.bidDown).Div(decTwo)
	e := mw.targetTiltVal.Sub(mw.moneyTiltVal)
	buyUp := e.Cmp(decZero) == 1

	var tokenID string
	var outcome model.Outcome
	if buyUp {
		tokenID = mw.market.tokenUp
		outcome = model.OutcomeUp
	} else {
		tokenID = mw.market.tokenDown
		outcome = model.OutcomeDown
	}

	mw.log.Info().
		Str("outcome", string(outcome)).
		Str("size", order.OriginalSize).
		Str("price", order.Price).
		Str("mid_up", midUp.String()).
		Str("mid_down", midDown.String()).
		Msg("placing order")

	placed, err := mw.client.PlaceOrder(tokenID, outcome, order.Price, order.OriginalSize)
	if err != nil {
		mw.log.Error().Err(err).Msg("failed to place order")
		return
	}

	// Persist order
	placed.ConditionID = mw.market.conditionID
	if err := mw.storage.InsertOrder(nil, placed); err != nil {
		mw.log.Error().Err(err).Msg("failed to insert order")
	}

	// Update position state after fill
	priceDec, _ := decimal.NewFromString(order.Price)
	sizeDec, _ := decimal.NewFromString(order.OriginalSize)
	if buyUp {
		newQ := mw.qUp.Add(sizeDec)
		if mw.qUp.IsZero() {
			mw.pUp = priceDec
		} else {
			mw.pUp = mw.qUp.Mul(mw.pUp).Add(sizeDec.Mul(priceDec)).Div(newQ)
		}
		mw.qUp = newQ
	} else {
		newQ := mw.qDown.Add(sizeDec)
		if mw.qDown.IsZero() {
			mw.pDown = priceDec
		} else {
			mw.pDown = mw.qDown.Mul(mw.pDown).Add(sizeDec.Mul(priceDec)).Div(newQ)
		}
		mw.qDown = newQ
	}
}

// processNewMarketState expects mw prices updated
func (mw *marketWorker) processNewMarketState() (bool, model.Order) {
	mw.tte = decimal.NewFromInt(time.Until(mw.market.endTime).Milliseconds())
	if mw.stopTradeTTEMs.Cmp(mw.tte) == 1 {
		return false, model.Order{}
	}

	midUp := mw.askUp.Add(mw.bidUp).Div(decTwo)
	midDown := mw.askDown.Add(mw.bidDown).Div(decTwo)

	mw.pFairVal = pFair(midUp, midDown)
	mw.moneyTiltVal = moneyTilt(mw.qUp, midUp, mw.qDown, midDown)
	mw.targetTiltVal = targetTilt(mw.pFairVal, mw.tte, mw.marketTimeMs,
		mw.kMin, mw.kMax, mw.alpha, mw.tauMin, mw.tauMax)

	mw.riskVal = risk(mw.qUp, mw.pUp, mw.qDown, mw.pDown)
	headroom := mw.riskLimit.Sub(mw.riskVal)
	if headroom.Cmp(decZero) == -1 {
		return false, model.Order{}
	}
	e := mw.targetTiltVal.Sub(mw.moneyTiltVal)
	eps := epsTTE(mw.tte, mw.marketTimeMs, mw.epsMin, mw.epsMax)
	if e.Abs().Cmp(eps) != 1 {
		return false, model.Order{}
	}
	if decimal.NewFromInt(time.Since(mw.lastDecision).Milliseconds()).Cmp(mw.decisionDebounceMs) != 1 {
		return false, model.Order{}
	}

	buyUp := e.Cmp(decZero) == 1
	var price decimal.Decimal
	if buyUp {
		price = chooseLimitPrice(mw.bidUp, mw.askUp, e, mw.tte, mw.marketTimeMs,
			mw.kMin, mw.kMax, mw.alpha, mw.eMax)
	} else {
		price = chooseLimitPrice(mw.bidDown, mw.askDown, e, mw.tte, mw.marketTimeMs,
			mw.kMin, mw.kMax, mw.alpha, mw.eMax)
	}

	dRisk := decimal.Min(mw.maxStepRisk, headroom)
	qRaw := dRisk.Div(price)
	qBuy := decimal.Max(qRaw, mw.minSize)

	for qBuy.Cmp(mw.minSize) != -1 {
		r2 := simulateNewRisk(mw.qUp, mw.pUp, mw.qDown, mw.pDown, buyUp, qBuy, price)
		if r2.Cmp(mw.riskLimit) == -1 {
			break
		}
		qBuy = qBuy.Sub(decHalf)
	}

	if qBuy.Cmp(mw.minSize) == -1 {
		return false, model.Order{}
	}

	mw.lastDecision = time.Now()

	return true, model.Order{
		OriginalSize: qBuy.StringFixed(2),
		Price:        price.StringFixed(2),
	}
}

func (mw *marketWorker) close() {
	mw.closeOnce.Do(func() {
		close(mw.closed)
	})
}
