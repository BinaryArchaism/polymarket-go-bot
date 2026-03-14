package polymarket

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

func (c *Client) Redeem(ctx context.Context, conditionID string) (int64, error) {
	redeemData, err := c.prepareRedeemData(conditionID)
	if err != nil {
		return 0, fmt.Errorf("prepare redeem data: %w", err)
	}

	safeAddr := common.HexToAddress(c.cfg.Polymarket.ProxyWalletAddress)
	safeNonce, err := getSafeNonce(ctx, c.ethClient, safeAddr)
	if err != nil {
		return 0, fmt.Errorf("get safe nonce: %w", err)
	}

	ctfAddr := common.HexToAddress(c.cfg.Polymarket.ConditionTokenAddress)
	typedData := buildSafeTxTypedData(safeAddr, ctfAddr, redeemData, safeNonce, PolygonChainID)

	safeTxHash, err := eip712Digest(typedData)
	if err != nil {
		return 0, fmt.Errorf("compute EIP-712 digest: %w", err)
	}

	owners := []ownerKey{
		{
			Addr: c.ownerAddress,
			Key:  c.privKey,
		},
	}

	signatures, err := signSafeTxHash(safeTxHash, owners)
	if err != nil {
		return 0, fmt.Errorf("sign safe tx: %w", err)
	}

	relayerKey, err := crypto.HexToECDSA(strings.TrimPrefix(c.cfg.Blockchain.PrivateKey, "0x"))
	if err != nil {
		return 0, fmt.Errorf("parse relayer key: %w", err)
	}

	tx, err := executeSafeTransaction(ctx, c.ethClient, safeAddr, ctfAddr, redeemData, signatures, relayerKey, PolygonChainID)
	if err != nil {
		return 0, fmt.Errorf("execute safe transaction: %w", err)
	}

	receipt, err := waitForTransaction(ctx, c.ethClient, tx.Hash())
	if err != nil {
		return 0, fmt.Errorf("wait for transaction: %w", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return 0, fmt.Errorf("transaction failed with status %d", receipt.Status)
	}

	payout, err := parsePayoutRedemptionEvent(receipt.Logs, conditionID, ctfAddr)
	if err != nil {
		return 0, fmt.Errorf("parse payout: %w", err)
	}

	return payout, nil
}

func (c *Client) prepareRedeemData(conditionID string) ([]byte, error) {
	ctfABI, err := abi.JSON(strings.NewReader(ConditionalTokensABI))
	if err != nil {
		return nil, fmt.Errorf("parse CTF ABI: %w", err)
	}

	indexSets := []*big.Int{big.NewInt(1), big.NewInt(2)}
	usdcAddr := common.HexToAddress(c.cfg.Polymarket.USDCeAddress)
	parentCollectionID := common.HexToHash(ParentCollectionID)
	conditionIDHash := common.HexToHash(conditionID)

	data, err := ctfABI.Pack("redeemPositions",
		usdcAddr,
		parentCollectionID,
		conditionIDHash,
		indexSets,
	)
	if err != nil {
		return nil, fmt.Errorf("pack redeem positions: %w", err)
	}

	return data, nil
}

func eip712Digest(td apitypes.TypedData) ([]byte, error) {
	domainSeparator, err := td.HashStruct("EIP712Domain", td.Domain.Map())
	if err != nil {
		return nil, err
	}
	msgHash, err := td.HashStruct(td.PrimaryType, td.Message)
	if err != nil {
		return nil, err
	}
	b := []byte{0x19, 0x01}
	b = append(b, domainSeparator...)
	b = append(b, msgHash...)
	return crypto.Keccak256(b), nil
}
