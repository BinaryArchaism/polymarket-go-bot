package polymarket

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const (
	polyChainID = 137
	domainName  = "ClobAuthDomain"
	version     = "1"
	msgText     = "This message attests that I control the given wallet"

	// CTF Exchange domain for order signing
	ctfExchangeDomainName    = "Polymarket CTF Exchange"
	ctfExchangeDomainVersion = "1"
)

// L2Auth holds API credentials for L2 authentication.
type L2Auth struct {
	APIKey     string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// SignatureType represents the wallet signature type.
type SignatureType int

const (
	SignatureTypeEOA        SignatureType = 0 // Standard EOA wallet
	SignatureTypePolyProxy  SignatureType = 1 // Magic Link proxy
	SignatureTypeGnosisSafe SignatureType = 2 // Gnosis Safe multisig
)

// OrderSideCLOB represents order side for CLOB API.
type OrderSideCLOB string

const (
	OrderSideBuy  OrderSideCLOB = "BUY"
	OrderSideSell OrderSideCLOB = "SELL"
)

// ToInt converts OrderSideCLOB to its integer representation for EIP-712 signing.
func (s OrderSideCLOB) ToInt() int {
	if s == OrderSideBuy {
		return 0
	}
	return 1
}

func (s OrderSideCLOB) ToIntStr() string {
	if s == OrderSideBuy {
		return "0"
	}
	return "1"
}

// TimeInForce represents order time-in-force.
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC" // Good 'til Cancelled
	TimeInForceGTD TimeInForce = "GTD" // Good 'til Date
	TimeInForceFOK TimeInForce = "FOK" // Fill or Kill
	TimeInForceFAK TimeInForce = "FAK" // Fill and Kill (IOC)
)

// JSONNumber is a string that marshals to JSON as a number (without quotes).
type JSONNumber string

// MarshalJSON implements json.Marshaler to output the number without quotes.
func (n JSONNumber) MarshalJSON() ([]byte, error) {
	if n == "" {
		return []byte("0"), nil
	}
	return []byte(n), nil
}

// CLOBOrder represents an order to be signed and submitted.
type CLOBOrder struct {
	Salt          string        `json:"salt"`
	Maker         string        `json:"maker"`
	Signer        string        `json:"signer"`
	Taker         string        `json:"taker"`
	TokenID       string        `json:"tokenId"`
	MakerAmount   string        `json:"makerAmount"`
	TakerAmount   string        `json:"takerAmount"`
	Expiration    string        `json:"expiration"`
	Nonce         string        `json:"nonce"`
	FeeRateBps    string        `json:"feeRateBps"`
	Side          OrderSideCLOB `json:"side"`
	SignatureType SignatureType `json:"signatureType"`
	Signature     string        `json:"signature"`
}

// OrderRequest is the request body for POST /order.
type OrderRequest struct {
	Order     CLOBOrder   `json:"order"`
	Owner     string      `json:"owner"`
	OrderType TimeInForce `json:"orderType"`
}

// OrderResponse is the response from POST /order.
type OrderResponse struct {
	Success     bool     `json:"success"`
	ErrorMsg    string   `json:"errorMsg,omitempty"`
	OrderID     string   `json:"orderID,omitempty"`
	OrderHashes []string `json:"orderHashes,omitempty"`
}

// CancelResponse is the response from DELETE /order endpoints.
type CancelResponse struct {
	Canceled    []string          `json:"canceled"`
	NotCanceled map[string]string `json:"not_canceled"`
}

// buildClobAuthTypedData builds the EIP-712 typed data for L1 auth.
func buildClobAuthTypedData(addr common.Address, timestamp string, nonce *big.Int) apitypes.TypedData {
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
			},
			"ClobAuth": []apitypes.Type{
				{Name: "address", Type: "address"},
				{Name: "timestamp", Type: "string"},
				{Name: "nonce", Type: "uint256"},
				{Name: "message", Type: "string"},
			},
		},
		PrimaryType: "ClobAuth",
		Domain: apitypes.TypedDataDomain{
			Name:    domainName,
			Version: version,
			ChainId: math.NewHexOrDecimal256(polyChainID),
		},
		Message: apitypes.TypedDataMessage{
			"address":   addr.Hex(),
			"timestamp": timestamp,
			"nonce":     nonce.String(),
			"message":   msgText,
		},
	}
}

// buildOrderTypedData builds the EIP-712 typed data for order signing.
func buildOrderTypedData(order *CLOBOrder, verifyingContract string) apitypes.TypedData {
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Order": []apitypes.Type{
				{Name: "salt", Type: "uint256"},
				{Name: "maker", Type: "address"},
				{Name: "signer", Type: "address"},
				{Name: "taker", Type: "address"},
				{Name: "tokenId", Type: "uint256"},
				{Name: "makerAmount", Type: "uint256"},
				{Name: "takerAmount", Type: "uint256"},
				{Name: "expiration", Type: "uint256"},
				{Name: "nonce", Type: "uint256"},
				{Name: "feeRateBps", Type: "uint256"},
				{Name: "side", Type: "uint8"},
				{Name: "signatureType", Type: "uint8"},
			},
		},
		PrimaryType: "Order",
		Domain: apitypes.TypedDataDomain{
			Name:              ctfExchangeDomainName,
			Version:           ctfExchangeDomainVersion,
			ChainId:           math.NewHexOrDecimal256(polyChainID),
			VerifyingContract: verifyingContract,
		},
		Message: apitypes.TypedDataMessage{
			"salt":          string(order.Salt),
			"maker":         order.Maker,
			"signer":        order.Signer,
			"taker":         order.Taker,
			"tokenId":       order.TokenID,
			"makerAmount":   order.MakerAmount,
			"takerAmount":   order.TakerAmount,
			"expiration":    order.Expiration,
			"nonce":         order.Nonce,
			"feeRateBps":    order.FeeRateBps,
			"side":          fmt.Sprintf("%d", order.Side.ToInt()),
			"signatureType": fmt.Sprintf("%d", order.SignatureType),
		},
	}
}

// eip712Hash computes the EIP-712 hash of typed data.
func eip712Hash(typedData apitypes.TypedData) ([]byte, error) {
	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return nil, fmt.Errorf("hash domain: %w", err)
	}

	msgHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return nil, fmt.Errorf("hash message: %w", err)
	}

	data := []byte{0x19, 0x01}
	data = append(data, domainSeparator...)
	data = append(data, msgHash...)

	return crypto.Keccak256(data), nil
}

// signEIP712 signs typed data with the given private key.
func signEIP712(typedData apitypes.TypedData, pk *ecdsa.PrivateKey) (string, error) {
	digest, err := eip712Hash(typedData)
	if err != nil {
		return "", fmt.Errorf("build eip712 hash: %w", err)
	}

	sig, err := crypto.Sign(digest, pk)
	if err != nil {
		return "", fmt.Errorf("sign typed data: %w", err)
	}

	// Adjust v value for Ethereum compatibility (27/28 instead of 0/1)
	if sig[64] < 27 {
		sig[64] += 27
	}

	return "0x" + hex.EncodeToString(sig), nil
}

// GenerateL1Headers generates L1 authentication headers for a request.
func GenerateL1Headers(pk *ecdsa.PrivateKey, nonce *big.Int) (map[string]string, error) {
	if nonce == nil {
		nonce = big.NewInt(0)
	}

	addr := crypto.PubkeyToAddress(pk.PublicKey)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	typedData := buildClobAuthTypedData(addr, timestamp, nonce)
	sig, err := signEIP712(typedData, pk)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"POLY_ADDRESS":   addr.Hex(),
		"POLY_SIGNATURE": sig,
		"POLY_TIMESTAMP": timestamp,
		"POLY_NONCE":     nonce.String(),
	}, nil
}

// SetL1Headers sets L1 authentication headers on an HTTP request.
func SetL1Headers(req *http.Request, pk *ecdsa.PrivateKey, nonce *big.Int) error {
	headers, err := GenerateL1Headers(pk, nonce)
	if err != nil {
		return err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return nil
}

func GenerateL2Headers(l2Auth L2Auth, method, path string, body []byte) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	// build the prehash string
	prehash := ts + method + path + string(body)

	// get secret bytes (API secret is usually base64 already, so decode)
	secretBytes, err := base64.StdEncoding.DecodeString(l2Auth.Secret)
	if err != nil {
		// if the stored secret is not base64, try raw bytes
		secretBytes = []byte(l2Auth.Secret)
	}

	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(prehash))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return map[string]string{
		"POLY_API_KEY":       l2Auth.APIKey,
		"POLY_API_SIGNATURE": signature,
		"POLY_API_TIMESTAMP": ts,
		"POLY_PASSPHRASE":    l2Auth.Passphrase,
	}
}

// SetL2Headers sets L2 authentication headers on an HTTP request.
func SetL2Headers(req *http.Request, l2Auth L2Auth, address string, body []byte) {
	headers, err := BuildL2Headers(l2Auth, address, req, body)
	if err != nil {
		log.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
}

// SignOrder signs a CLOB order using EIP-712.
func SignOrder(order *CLOBOrder, pk *ecdsa.PrivateKey, verifyingContract string) (string, error) {
	typedData := buildOrderTypedData(order, verifyingContract)
	return signEIP712(typedData, pk)
}

func BuildL2Headers(l2 L2Auth, walletAddress string, req *http.Request, body []byte) (map[string]string, error) {
	if walletAddress == "" || !strings.HasPrefix(walletAddress, "0x") {
		return nil, fmt.Errorf("walletAddress must be 0x...; got %q", walletAddress)
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	method := strings.ToUpper(req.Method)
	requestPath := req.URL.RequestURI()

	msg := timestamp + method + requestPath
	if len(body) > 0 {
		msg += string(body)
	}

	secretBytes, err := decodeURLSafeBase64(l2.Secret)
	if err != nil {
		return nil, fmt.Errorf("decode L2 secret (urlsafe base64): %w", err)
	}

	mac := hmac.New(sha256.New, secretBytes)
	_, _ = mac.Write([]byte(msg))
	sigRaw := mac.Sum(nil)

	signature := base64.URLEncoding.EncodeToString(sigRaw)

	return map[string]string{
		"POLY_API_KEY":    l2.APIKey,
		"POLY_PASSPHRASE": l2.Passphrase,
		"POLY_SIGNATURE":  signature,
		"POLY_TIMESTAMP":  timestamp,
		"POLY_ADDRESS":    walletAddress,
	}, nil
}

func decodeURLSafeBase64(s string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}
