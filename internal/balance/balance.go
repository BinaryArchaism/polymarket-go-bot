package balance

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/config"
)

const erc20ABI = `[
	{
		"constant": true,
		"inputs": [{"name": "owner", "type": "address"}],
		"name": "balanceOf",
		"outputs": [{"name": "", "type": "uint256"}],
		"type": "function"
	}
]`

const updateInterval = 5 * time.Minute

var usdcDivisor = decimal.NewFromInt(1_000_000) // USDC has 6 decimals

type Balance struct {
	balance decimal.Decimal

	log zerolog.Logger

	cli       *ethclient.Client
	callData  []byte
	parsedABI abi.ABI
	addressTo common.Address
}

func New(ctx context.Context, cfg config.Config, logger zerolog.Logger) (*Balance, error) {
	parsedABI, err := abi.JSON(strings.NewReader(erc20ABI))
	if err != nil {
		return nil, err
	}
	data, err := parsedABI.Pack("balanceOf",
		common.HexToAddress(cfg.Polymarket.ProxyWalletAddress))
	if err != nil {
		return nil, err
	}
	cli, err := ethclient.Dial(cfg.Blockchain.PolygonRPC)
	if err != nil {
		return nil, fmt.Errorf("can not create poly client: %w", err)
	}

	bal := &Balance{
		log:       logger.With().Str("component", "balance").Logger(),
		cli:       cli,
		callData:  data,
		parsedABI: parsedABI,
		addressTo: common.HexToAddress(cfg.Polymarket.USDCeAddress),
	}

	if err = bal.Update(); err != nil {
		return nil, err
	}

	go bal.updateWorker(ctx)

	return bal, nil
}

func (b *Balance) updateWorker(ctx context.Context) {
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.Update()
		}
	}
}

func (b *Balance) GetBalance() decimal.Decimal {
	return b.balance
}

func (b *Balance) IncreaseBalance(add decimal.Decimal) {
	b.balance = b.balance.Add(add)
}

func (b *Balance) DecreaseBalance(sub decimal.Decimal) {
	b.balance = b.balance.Sub(sub)
}

func (b *Balance) Update() error {
	res, err := b.cli.CallContract(context.Background(), ethereum.CallMsg{
		To:   &b.addressTo,
		Data: b.callData,
	}, nil)
	if err != nil {
		return err
	}
	out, err := b.parsedABI.Unpack("balanceOf", res)
	if err != nil {
		return err
	}

	raw := decimal.NewFromBigInt(out[0].(*big.Int), 0)
	newBalance := raw.Div(usdcDivisor)

	if b.balance.Equal(newBalance) {
		return nil
	}

	b.log.Info().
		Str("old_balance", b.balance.String()).
		Str("new_balance", newBalance.String()).
		Msg("balance changed")

	b.balance = newBalance

	return nil
}
