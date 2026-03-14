package polymarket

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// getSafeNonce retrieves the current nonce from a Gnosis Safe contract
func getSafeNonce(ctx context.Context, ethClient *ethclient.Client, safeAddr common.Address) (*big.Int, error) {
	safeABI, err := abi.JSON(strings.NewReader(SafeABI))
	if err != nil {
		return nil, fmt.Errorf("parse safe ABI: %w", err)
	}

	nonceCall, err := safeABI.Pack("nonce")
	if err != nil {
		return nil, fmt.Errorf("pack nonce call: %w", err)
	}

	out, err := ethClient.CallContract(ctx, ethereum.CallMsg{To: &safeAddr, Data: nonceCall}, nil)
	if err != nil {
		return nil, fmt.Errorf("call contract: %w", err)
	}

	decoded, err := safeABI.Unpack("nonce", out)
	if err != nil {
		return nil, fmt.Errorf("unpack nonce: %w", err)
	}

	nonce, ok := decoded[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid nonce type")
	}

	return nonce, nil
}

// signSafeTxHash signs a Safe transaction hash using EIP-712 and returns concatenated signatures
func signSafeTxHash(safeTxHash []byte, owners []ownerKey) ([]byte, error) {
	// Sort owners by address to ensure deterministic signature order
	sort.Slice(owners, func(i, j int) bool {
		return owners[i].Addr.Hex() < owners[j].Addr.Hex()
	})

	var sigs []byte
	for _, o := range owners {
		sig, err := crypto.Sign(safeTxHash, o.Key)
		if err != nil {
			return nil, fmt.Errorf("sign owner %s: %w", o.Addr.Hex(), err)
		}
		// Adjust v value to 27/28 for EIP-155
		if sig[64] < 27 {
			sig[64] += 27
		}
		sigs = append(sigs, sig...)
	}

	return sigs, nil
}

// ownerKey represents a Safe owner with their address and private key
type ownerKey struct {
	Addr common.Address
	Key  *ecdsa.PrivateKey
}

// buildSafeTxTypedData creates an EIP-712 typed data structure for a Safe transaction
func buildSafeTxTypedData(
	safeAddr common.Address,
	to common.Address,
	data []byte,
	nonce *big.Int,
	chainID int64,
) apitypes.TypedData {
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"SafeTx": {
				{Name: "to", Type: "address"},
				{Name: "value", Type: "uint256"},
				{Name: "data", Type: "bytes"},
				{Name: "operation", Type: "uint8"},
				{Name: "safeTxGas", Type: "uint256"},
				{Name: "baseGas", Type: "uint256"},
				{Name: "gasPrice", Type: "uint256"},
				{Name: "gasToken", Type: "address"},
				{Name: "refundReceiver", Type: "address"},
				{Name: "nonce", Type: "uint256"},
			},
		},
		PrimaryType: "SafeTx",
		Domain: apitypes.TypedDataDomain{
			ChainId:           (*math.HexOrDecimal256)(big.NewInt(chainID)),
			VerifyingContract: safeAddr.Hex(),
		},
		Message: apitypes.TypedDataMessage{
			"to":             to.Hex(),
			"value":          "0",
			"data":           data,
			"operation":      "0",
			"safeTxGas":      "0",
			"baseGas":        "0",
			"gasPrice":       "0",
			"gasToken":       common.Address{}.Hex(),
			"refundReceiver": common.Address{}.Hex(),
			"nonce":          nonce.String(),
		},
	}
}

// executeSafeTransaction packs and sends a Safe execTransaction to the blockchain
func executeSafeTransaction(
	ctx context.Context,
	ethClient *ethclient.Client,
	safeAddr common.Address,
	to common.Address,
	redeemData []byte,
	signatures []byte,
	relayerKey *ecdsa.PrivateKey,
	chainID int64,
) (*types.Transaction, error) {
	safeABI, err := abi.JSON(strings.NewReader(SafeABI))
	if err != nil {
		return nil, fmt.Errorf("parse safe ABI: %w", err)
	}

	execData, err := safeABI.Pack("execTransaction",
		to,
		big.NewInt(0),
		redeemData,
		uint8(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(0),
		common.Address{}, common.Address{},
		signatures,
	)
	if err != nil {
		return nil, fmt.Errorf("pack execTransaction: %w", err)
	}

	relayer := crypto.PubkeyToAddress(relayerKey.PublicKey)

	// Retry logic for nonce collisions
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retrying to allow pending transactions to be mined
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}

		nonce, err := ethClient.NonceAt(ctx, relayer, nil)
		if err != nil {
			return nil, fmt.Errorf("get nonce: %w", err)
		}

		tip, err := ethClient.SuggestGasTipCap(ctx)
		if err != nil {
			return nil, fmt.Errorf("suggest gas tip: %w", err)
		}

		fee, err := ethClient.SuggestGasPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("suggest gas price: %w", err)
		}

		// Boost gas prices on retries to ensure transaction replacement
		if attempt > 0 {
			boost := big.NewInt(int64(10 + attempt*10)) // 10%, 20%, 30% boost
			tip = new(big.Int).Add(tip, new(big.Int).Div(new(big.Int).Mul(tip, boost), big.NewInt(100)))
			fee = new(big.Int).Add(fee, new(big.Int).Div(new(big.Int).Mul(fee, boost), big.NewInt(100)))
		}

		msg := ethereum.CallMsg{From: relayer, To: &safeAddr, Data: execData}
		gas, err := ethClient.EstimateGas(ctx, msg)
		if err != nil {
			// Fallback to a safe gas limit if estimation fails
			gas = 500_000
		}

		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   big.NewInt(chainID),
			Nonce:     nonce,
			To:        &safeAddr,
			Value:     big.NewInt(0),
			Gas:       gas,
			GasTipCap: tip,
			GasFeeCap: fee,
			Data:      execData,
		})

		signed, err := types.SignTx(tx, types.NewLondonSigner(big.NewInt(chainID)), relayerKey)
		if err != nil {
			return nil, fmt.Errorf("sign transaction: %w", err)
		}

		err = ethClient.SendTransaction(ctx, signed)
		if err == nil {
			return signed, nil
		}

		lastErr = err
		errMsg := err.Error()

		// Check if this is a nonce-related error that we should retry
		if strings.Contains(errMsg, "replacement transaction underpriced") ||
			strings.Contains(errMsg, "nonce too low") ||
			strings.Contains(errMsg, "already known") {
			continue // Retry
		}

		// For other errors, fail immediately
		return nil, fmt.Errorf("send transaction: %w", err)
	}

	return nil, fmt.Errorf("send transaction after %d attempts: %w", maxRetries, lastErr)
}

// waitForTransaction waits for a transaction to be mined
func waitForTransaction(ctx context.Context, ethClient *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		receipt, err := ethClient.TransactionReceipt(ctx, txHash)
		if err == nil {
			return receipt, nil
		}

		if err != ethereum.NotFound {
			return nil, fmt.Errorf("get transaction receipt: %w", err)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// Continue waiting
		}
	}
}

// parsePayoutRedemptionEvent parses the PayoutRedemption event from transaction logs
func parsePayoutRedemptionEvent(
	logs []*types.Log,
	conditionID string,
	ctfAddress common.Address,
) (int64, error) {
	eventABI, err := abi.JSON(strings.NewReader(PayoutRedemptionEventABI))
	if err != nil {
		return 0, fmt.Errorf("parse event ABI: %w", err)
	}

	// Get the event signature hash
	eventSig := eventABI.Events["PayoutRedemption"].ID

	// Convert conditionID to bytes32 for comparison
	var conditionIDBytes32 [32]byte
	conditionIDHex := strings.TrimPrefix(conditionID, "0x")
	conditionIDData := common.Hex2Bytes(conditionIDHex)
	copy(conditionIDBytes32[32-len(conditionIDData):], conditionIDData)

	for _, log := range logs {
		// Check if this is a PayoutRedemption event from the CTF contract
		if log.Address != ctfAddress || len(log.Topics) == 0 || log.Topics[0] != eventSig {
			continue
		}

		// Parse the event
		event := struct {
			ConditionId [32]byte
			IndexSets   []*big.Int
			Payout      *big.Int
		}{}

		err := eventABI.UnpackIntoInterface(&event, "PayoutRedemption", log.Data)
		if err != nil {
			continue
		}

		// Check if this event matches our conditionID
		if event.ConditionId != conditionIDBytes32 {
			continue
		}

		// Convert payout from USDC (6 decimals) to cents
		// USDC has 6 decimals, cents are 2 decimals, so we need to divide by 10^4
		payout := event.Payout
		if payout == nil {
			continue
		}

		// Convert to cents: payout / 10^4
		divisor := big.NewInt(10000)
		payoutCents := new(big.Int).Div(payout, divisor)

		return payoutCents.Int64(), nil
	}

	return 0, fmt.Errorf("PayoutRedemption event not found for condition %s", conditionID)
}
