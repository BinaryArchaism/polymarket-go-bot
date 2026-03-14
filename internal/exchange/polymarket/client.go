package polymarket

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/config"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/model"
)

type Client struct {
	cfg        config.Config
	mockOrders bool

	ethClient    *ethclient.Client
	privKey      *ecdsa.PrivateKey
	ownerAddress common.Address

	l2Creds *L2Auth
}

type ClientOption func(*Client)

func MockOrder(c *Client) {
	c.mockOrders = true
}

type apiKeyCredsResp struct {
	APIKey     string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

func New(cfg config.Config, opts ...ClientOption) (*Client, error) {
	ethcli, err := ethclient.Dial(cfg.Blockchain.PolygonRPC)
	if err != nil {
		return nil, fmt.Errorf("can not dial connection to polygon: %w", err)
	}

	priv, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.Blockchain.PrivateKey, "0x"))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	from := crypto.PubkeyToAddress(priv.PublicKey)

	cli := &Client{
		mockOrders:   false,
		ethClient:    ethcli,
		privKey:      priv,
		ownerAddress: from,
		cfg:          cfg,
	}

	l2, err := cli.getOrDeriveL2Creds()
	if err != nil {
		return nil, fmt.Errorf("getOrDeriveL2Creds: %w", err)
	}
	cli.l2Creds = &l2

	for _, opt := range opts {
		opt(cli)
	}

	return cli, nil
}

func (c *Client) getOrDeriveL2Creds() (L2Auth, error) {
	if c.l2Creds != nil {
		return *c.l2Creds, nil
	}

	base := strings.TrimRight(c.cfg.Polymarket.CLOBURL, "/")

	creds, err := c.callL1GetApiCreds(base + "/auth/derive-api-key")
	if err == nil {
		c.l2Creds = &creds
		return creds, nil
	}

	creds2, err2 := c.callL1PostApiCreds(base + "/auth/api-key")
	if err2 != nil {
		return L2Auth{}, fmt.Errorf("derive api creds failed: %v; create api creds failed: %w", err, err2)
	}

	c.l2Creds = &creds2
	return creds2, nil
}

func (c *Client) callL1GetApiCreds(url string) (L2Auth, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return L2Auth{}, err
	}

	if err := SetL1Headers(req, c.privKey, nil); err != nil {
		return L2Auth{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return L2Auth{}, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return L2Auth{}, fmt.Errorf("L1 GET %s status=%d body=%s", url, resp.StatusCode, string(b))
	}

	var r apiKeyCredsResp
	if err := json.Unmarshal(b, &r); err != nil {
		return L2Auth{}, fmt.Errorf("decode creds: %w body=%s", err, string(b))
	}
	if r.APIKey == "" || r.Secret == "" || r.Passphrase == "" {
		return L2Auth{}, fmt.Errorf("empty creds from %s body=%s", url, string(b))
	}

	return L2Auth{APIKey: r.APIKey, Secret: r.Secret, Passphrase: r.Passphrase}, nil
}

func (c *Client) callL1PostApiCreds(url string) (L2Auth, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return L2Auth{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	if err := SetL1Headers(req, c.privKey, nil); err != nil {
		return L2Auth{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return L2Auth{}, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return L2Auth{}, fmt.Errorf("L1 POST %s status=%d body=%s", url, resp.StatusCode, string(b))
	}

	var r apiKeyCredsResp
	if err := json.Unmarshal(b, &r); err != nil {
		return L2Auth{}, fmt.Errorf("decode creds: %w body=%s", err, string(b))
	}

	return L2Auth{APIKey: r.APIKey, Secret: r.Secret, Passphrase: r.Passphrase}, nil
}

func (c *Client) GetMarkets(ctx context.Context) ([]model.Market, error) {
	url := fmt.Sprintf("%s/events?active=true&archived=false&tag_id=102467&order=startDate&limit=36&closed=false&offset=0",
		c.cfg.Polymarket.GammaURL)

	type market struct {
		ID             string    `json:"id"`
		Question       string    `json:"question"`
		ConditionID    string    `json:"conditionId"`
		Slug           string    `json:"slug"`
		EndDate        time.Time `json:"endDate"`
		Outcomes       string    `json:"outcomes"`
		ClobTokenIds   string    `json:"clobTokenIds"`
		EventStartTime time.Time `json:"eventStartTime"`
	}

	type event struct {
		ID      string   `json:"id"`
		Markets []market `json:"markets"`
	}

	var jsonResp []event

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &jsonResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	res := make([]model.Market, 0, 12)
	for _, evt := range jsonResp {
		for _, m := range evt.Markets {
			outcomesStr := strings.Trim(m.Outcomes, "[]")
			outcomesStr = strings.ReplaceAll(outcomesStr, "\"", "")
			outcomes := strings.Split(outcomesStr, ", ")
			if len(outcomes) != 2 {
				continue
			}

			tokenIDsStr := strings.Trim(m.ClobTokenIds, "[]")
			tokenIDsStr = strings.ReplaceAll(tokenIDsStr, "\"", "")
			tokenIDs := strings.Split(tokenIDsStr, ", ")
			if len(tokenIDs) != 2 {
				continue
			}

			underlying := extractUnderlying(m.Slug)

			res = append(res, model.Market{
				ID:          m.ID,
				EventID:     evt.ID,
				Status:      model.MarketStatusPlanned,
				Question:    m.Question,
				ConditionID: m.ConditionID,
				Underlying:  underlying,
				Slug:        m.Slug,
				TokenUp:     strings.TrimSpace(tokenIDs[0]),
				TokenDown:   strings.TrimSpace(tokenIDs[1]),
				StartTime:   m.EventStartTime,
				EndTime:     m.EndDate,
			})
		}
	}

	return res, nil
}

func extractUnderlying(slug string) model.UnderlyingType {
	slugLower := strings.ToLower(slug)

	if strings.Contains(slugLower, "btc") || strings.Contains(slugLower, "bitcoin") {
		return model.UnderlyingBTC
	}
	if strings.Contains(slugLower, "eth") || strings.Contains(slugLower, "ethereum") {
		return model.UnderlyingETH
	}
	if strings.Contains(slugLower, "sol") || strings.Contains(slugLower, "solana") {
		return model.UnderlyingSOL
	}
	if strings.Contains(slugLower, "xrp") || strings.Contains(slugLower, "ripple") {
		return model.UnderlyingXRP
	}

	return model.UnderlyingBTC
}

func (c *Client) GetPrices(tokenUp string, tokenDown string) (model.Price, error) {
	reqBody := []struct {
		TokenID string `json:"token_id"`
		Side    string `json:"side"`
	}{
		{TokenID: tokenUp, Side: "BUY"},
		{TokenID: tokenUp, Side: "SELL"},
		{TokenID: tokenDown, Side: "BUY"},
		{TokenID: tokenDown, Side: "SELL"},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return model.Price{}, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/prices", c.cfg.Polymarket.CLOBURL)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return model.Price{}, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.Price{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return model.Price{}, fmt.Errorf("prices API error: status=%d, body=%s", resp.StatusCode, string(body))
	}

	jsonBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.Price{}, fmt.Errorf("read response: %w", err)
	}

	var pricesResp map[string]struct {
		Buy  string `json:"BUY"`
		Sell string `json:"SELL"`
	}

	if err := json.Unmarshal(jsonBody, &pricesResp); err != nil {
		return model.Price{}, fmt.Errorf("unmarshal response: %w", err)
	}

	upPrices, ok := pricesResp[tokenUp]
	if !ok {
		return model.Price{}, fmt.Errorf("token up %s not found in response", tokenUp)
	}
	upAsk, err := decimal.NewFromString(upPrices.Buy)
	if err != nil {
		return model.Price{}, fmt.Errorf("parse up ask price: %w", err)
	}
	upBid, err := decimal.NewFromString(upPrices.Sell)
	if err != nil {
		return model.Price{}, fmt.Errorf("parse up bid price: %w", err)
	}

	downPrices, ok := pricesResp[tokenDown]
	if !ok {
		return model.Price{}, fmt.Errorf("token down %s not found in response", tokenDown)
	}
	downAsk, err := decimal.NewFromString(downPrices.Buy)
	if err != nil {
		return model.Price{}, fmt.Errorf("parse down ask price: %w", err)
	}
	downBid, err := decimal.NewFromString(downPrices.Sell)
	if err != nil {
		return model.Price{}, fmt.Errorf("parse down bid price: %w", err)
	}

	return model.Price{
		UpAsk:   upAsk,
		UpBid:   upBid,
		DownAsk: downAsk,
		DownBid: downBid,
	}, nil
}

func randomSaltDecimal64() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return new(big.Int).SetBytes(b[:]).String()
}

func (c *Client) GetOrder(ctx context.Context, orderHash string) (model.Order, error) {
	type OpenOrder struct {
		ID              string   `json:"id"`
		Status          string   `json:"status"`
		Market          string   `json:"market"`
		OriginalSize    string   `json:"original_size"`
		Outcome         string   `json:"outcome"`
		Price           string   `json:"price"`
		Side            string   `json:"side"`
		SizeMatched     string   `json:"size_matched"`
		AssetID         string   `json:"asset_id"`
		AssociateTrades []string `json:"associate_trades"`
	}

	base := strings.TrimRight(c.cfg.Polymarket.CLOBURL, "/")
	url := fmt.Sprintf("%s/data/order/%s", base, orderHash)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return model.Order{}, fmt.Errorf("create request: %w", err)
	}

	l2, err := c.getOrDeriveL2Creds()
	if err != nil {
		return model.Order{}, fmt.Errorf("getOrDeriveL2Creds: %w", err)
	}

	headers, err := BuildL2Headers(l2, c.ownerAddress.Hex(), req, nil)
	if err != nil {
		return model.Order{}, fmt.Errorf("BuildL2Headers: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.Order{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.Order{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return model.Order{}, fmt.Errorf("get order failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var openOrder OpenOrder
	if err := json.Unmarshal(body, &openOrder); err != nil {
		return model.Order{}, fmt.Errorf("unmarshal response: %w", err)
	}

	var tradeSide model.TradeSide
	if strings.EqualFold(openOrder.Side, "BUY") {
		tradeSide = model.TradeSideBuy
	} else {
		tradeSide = model.TradeSideSell
	}

	return model.Order{
		ID:           openOrder.ID,
		Status:       openOrder.Status,
		TokenID:      openOrder.AssetID,
		ConditionID:  openOrder.Market,
		TradeSide:    tradeSide,
		OriginalSize: openOrder.OriginalSize,
		MatchedSize:  openOrder.SizeMatched,
		Price:        openOrder.Price,
	}, nil
}

func (c *Client) PlaceOrder(tokenID string, outcome model.Outcome, price, size string) (model.Order, error) {
	if c.mockOrders {
		return model.Order{ID: uuid.New().String()}, nil
	}

	var out model.Order

	if tokenID == "" {
		return out, fmt.Errorf("tokenID is empty")
	}

	priceDec, err := decimal.NewFromString(price)
	if err != nil || priceDec.IsZero() || priceDec.IsNegative() {
		return out, fmt.Errorf("price must be > 0, got %s", price)
	}

	sizeDec, err := decimal.NewFromString(size)
	if err != nil || sizeDec.IsZero() || sizeDec.IsNegative() {
		return out, fmt.Errorf("size must be > 0, got %s", size)
	}

	signerEOA := c.ownerAddress
	if signerEOA == (common.Address{}) {
		signerEOA = crypto.PubkeyToAddress(c.privKey.PublicKey)
	}
	signer := signerEOA.Hex()

	proxy := common.HexToAddress(c.cfg.Polymarket.ProxyWalletAddress)
	if proxy == (common.Address{}) {
		return out, fmt.Errorf("invalid proxy_wallet_address=%q", c.cfg.Polymarket.ProxyWalletAddress)
	}
	maker := proxy.Hex()

	const taker = "0x0000000000000000000000000000000000000000"

	// Convert price/size to 1e6 precision for on-chain amounts
	// price is 0..1, size is number of contracts
	priceMicro := priceDec.Mul(decimal.NewFromInt(1_000_000)) // price per contract in micro-USDC
	sizeMicro := sizeDec.Mul(decimal.NewFromInt(1_000_000))   // size in micro-units
	totalUSDCMicro := sizeDec.Mul(priceMicro)                 // total USDC cost in micro

	makerAmount := totalUSDCMicro.Truncate(0).BigInt()
	takerAmount := sizeMicro.Truncate(0).BigInt()

	l2, err := c.getOrDeriveL2Creds()
	if err != nil {
		return out, fmt.Errorf("getOrDeriveL2Creds: %w", err)
	}

	order := CLOBOrder{
		Salt:          randomSaltDecimal64(),
		Maker:         maker,
		Signer:        signer,
		Taker:         taker,
		TokenID:       tokenID,
		MakerAmount:   makerAmount.String(),
		TakerAmount:   takerAmount.String(),
		Expiration:    "0",
		Nonce:         "0",
		FeeRateBps:    "0",
		Side:          OrderSideBuy,
		SignatureType: SignatureTypeGnosisSafe,
	}

	sig, err := SignOrder(&order, c.privKey, c.cfg.Polymarket.CTFExchangeAddress)
	if err != nil {
		return out, fmt.Errorf("SignOrder: %w", err)
	}
	order.Signature = sig

	type clobOrderPayload struct {
		Salt          int    `json:"salt"`
		Maker         string `json:"maker"`
		Signer        string `json:"signer"`
		Taker         string `json:"taker"`
		TokenID       string `json:"tokenId"`
		MakerAmount   string `json:"makerAmount"`
		TakerAmount   string `json:"takerAmount"`
		Expiration    string `json:"expiration"`
		Nonce         string `json:"nonce"`
		FeeRateBps    string `json:"feeRateBps"`
		Side          string `json:"side"`
		SignatureType int    `json:"signatureType"`
		Signature     string `json:"signature"`
	}

	payload := struct {
		Order     clobOrderPayload `json:"order"`
		Owner     string           `json:"owner"`
		OrderType string           `json:"orderType"`
	}{
		Order: clobOrderPayload{
			Salt:          func() int { v, _ := new(big.Int).SetString(order.Salt, 10); return int(v.Int64()) }(),
			Maker:         order.Maker,
			Signer:        order.Signer,
			Taker:         order.Taker,
			TokenID:       order.TokenID,
			MakerAmount:   order.MakerAmount,
			TakerAmount:   order.TakerAmount,
			Expiration:    order.Expiration,
			Nonce:         order.Nonce,
			FeeRateBps:    order.FeeRateBps,
			Side:          string(order.Side),
			SignatureType: int(order.SignatureType),
			Signature:     order.Signature,
		},
		Owner:     l2.APIKey,
		OrderType: string(TimeInForceFAK),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return out, fmt.Errorf("marshal order request: %w", err)
	}

	base := strings.TrimRight(c.cfg.Polymarket.CLOBURL, "/")
	req, err := http.NewRequest(http.MethodPost, base+"/order", bytes.NewReader(bodyBytes))
	if err != nil {
		return out, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	headers, err := BuildL2Headers(l2, signer, req, bodyBytes)
	if err != nil {
		return out, fmt.Errorf("BuildL2Headers: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)

	var or OrderResponse
	if err := json.Unmarshal(respBytes, &or); err != nil {
		return out, fmt.Errorf("decode response status=%d: %w; body=%s", resp.StatusCode, err, string(respBytes))
	}

	if !or.Success || or.ErrorMsg != "" {
		msg := or.ErrorMsg
		if msg == "" {
			msg = fmt.Sprintf("order rejected status=%d body=%s", resp.StatusCode, string(respBytes))
		}
		return out, fmt.Errorf("FAK order not executed: %s", msg)
	}
	if or.OrderID == "" {
		return out, fmt.Errorf("success=true but empty orderID; body=%s", string(respBytes))
	}

	return model.Order{
		ID:           or.OrderID,
		TokenID:      tokenID,
		TradeSide:    model.TradeSideBuy,
		Price:        price,
		OriginalSize: size,
		Outcome:      outcome,
	}, nil
}
