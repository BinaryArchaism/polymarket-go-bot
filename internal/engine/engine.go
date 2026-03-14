package engine

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/config"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/exchange"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/model"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/storage"
)

type Engine struct {
	config  config.Config
	client  exchange.Client
	storage storage.Storage
	log     zerolog.Logger

	wgMarket      sync.WaitGroup
	mutex         sync.RWMutex
	marketWorkers map[string]context.CancelFunc
}

func NewEngine(
	logger zerolog.Logger,
	cfg config.Config,
	cli exchange.Client,
	stg storage.Storage,
) *Engine {
	return &Engine{
		config:        cfg,
		client:        cli,
		storage:       stg,
		log:           logger.With().Str("component", "engine").Logger(),
		marketWorkers: make(map[string]context.CancelFunc),
	}
}

func (e *Engine) Start(ctx context.Context) {
	go e.startRedeemWorker(ctx)
	go e.loop(ctx)
}

func (e *Engine) Stop() {
	e.mutex.RLock()
	for mID, cancel := range e.marketWorkers {
		e.log.Debug().Str("market_id", mID).Msg("closing market")
		cancel()
	}
	e.mutex.RUnlock()

	e.wgMarket.Wait()
	e.log.Info().Msg("all markets closed, shutdown")
}

func (e *Engine) loop(ctx context.Context) {
	ticker := time.NewTicker(e.config.Market.UpdateInterval)
	defer ticker.Stop()

	e.processMarkets(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.processMarkets(ctx)
		}
	}
}

func (e *Engine) processMarkets(ctx context.Context) {
	savedMarkets, err := e.storage.GetPlannedAndActiveMarkets()
	if err != nil {
		e.log.Error().Err(err).Msg("can not get markets from storage")
		return
	}
	markets, err := e.client.GetMarkets(ctx)
	if err != nil {
		e.log.Error().Err(err).Msg("can not get markets from client")
		return
	}
	filteredMarkets := e.filterMarkets(markets)

	uniqMarketID := sliceExistenceMap(savedMarkets)
	marketIDToStart := savedMarkets[:]
	for _, m := range filteredMarkets {
		if uniqMarketID[m.ID] {
			continue
		}
		marketIDToStart = append(marketIDToStart, m.ID)
		m.Status = model.MarketStatusPlanned
		err = e.storage.InsertMarket(nil, m)
		if err != nil {
			e.log.Error().Err(err).Msg("can not insert market")
			return
		}
	}

	e.mutex.Lock()
	for _, mID := range marketIDToStart {
		if _, exist := e.marketWorkers[mID]; exist {
			continue
		}
		mCtx, cancel := context.WithCancel(ctx)
		mw := newMarketWorker(e.config, e.client, e.storage, e.log, mID)
		e.wgMarket.Add(1)
		go func() {
			defer e.wgMarket.Done()
			for {
				err := mw.Run(mCtx, e.config.Market.PollInterval)
				if err == nil || mCtx.Err() != nil {
					break
				}
				e.log.Error().Err(err).Str("market_id", mID).Msg("market worker error, retrying")
				time.Sleep(time.Second)
			}
		}()
		e.marketWorkers[mID] = cancel
	}
	e.mutex.Unlock()
}

func (e *Engine) filterMarkets(markets []model.Market) []model.Market {
	filtered := make([]model.Market, 0)

	for _, m := range markets {
		if !e.config.IsAllowedUnderlying(string(m.Underlying)) {
			continue
		}
		timeToExpiry := time.Until(m.EndTime)

		if timeToExpiry <= 0 {
			continue
		}
		if e.config.Market.MinTimeToExpiry > 0 && timeToExpiry < e.config.Market.MinTimeToExpiry {
			continue
		}

		filtered = append(filtered, m)
	}

	return filtered
}

func (e *Engine) startRedeemWorker(ctx context.Context) {
	logger := e.log.With().Str("component", "redeem_worker").Logger()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	redeemFn := func() {
		resolvedMarkets, err := e.storage.GetResolvedMarkets()
		if err != nil {
			logger.Error().Err(err).Msg("can not get resolved markets")
			return
		}
		for _, m := range resolvedMarkets {
			if time.Since(m.EndTime) < 15*time.Minute {
				logger.Debug().Str("market_id", m.ID).
					Str("await", time.Until(m.EndTime.Add(15*time.Minute)).String()).
					Msg("waiting for redemption window")
				continue
			}

			rCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			_, err = e.client.Redeem(rCtx, m.ConditionID)
			cancel()
			if err != nil {
				logger.Error().Err(err).Str("market_id", m.ID).Msg("can not redeem market")
				continue
			}
			logger.Info().Str("market_id", m.ID).Msg("onchain redemption success")

			if err := e.storage.UpdateMarketStatus(nil, m.ID, model.MarketStatusRedeemed); err != nil {
				logger.Error().Err(err).Str("market_id", m.ID).Msg("can not set market redeemed")
				continue
			}

			logger.Info().Str("market_id", m.ID).Msg("market redeemed")

			// Delay between redemptions to avoid nonce collisions
			time.Sleep(2 * time.Second)
		}
	}

	redeemFn()

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("stop working")
			return
		case <-ticker.C:
			redeemFn()
		}
	}
}

func sliceExistenceMap[T comparable](slice []T) map[T]bool {
	res := make(map[T]bool, len(slice))
	for _, e := range slice {
		res[e] = true
	}
	return res
}
