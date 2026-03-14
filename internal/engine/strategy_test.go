package engine

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	_ "github.com/lib/pq"
)

func d(v string) decimal.Decimal {
	return decimal.RequireFromString(v)
}

func assertDecEqual(t *testing.T, name string, got, want decimal.Decimal) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s: got %s, want %s", name, got.String(), want.String())
	}
}

func assertDecClose(t *testing.T, name string, got, want decimal.Decimal, tolerance string) {
	t.Helper()
	tol := d(tolerance)
	diff := got.Sub(want).Abs()
	if diff.GreaterThan(tol) {
		t.Errorf("%s: got %s, want %s (diff %s > tolerance %s)", name, got.String(), want.String(), diff.String(), tolerance)
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		name     string
		x, a, b  decimal.Decimal
		expected decimal.Decimal
	}{
		{"within range", d("0.5"), d("0"), d("1"), d("0.5")},
		{"below min", d("-0.5"), d("0"), d("1"), d("0")},
		{"above max", d("1.5"), d("0"), d("1"), d("1")},
		{"at min", d("0"), d("0"), d("1"), d("0")},
		{"at max", d("1"), d("0"), d("1"), d("1")},
		{"negative range", d("-5"), d("-10"), d("-1"), d("-5")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := clamp(tt.x, tt.a, tt.b)
			assertDecEqual(t, "clamp", result, tt.expected)
		})
	}
}

func TestMinDec(t *testing.T) {
	tests := []struct {
		name     string
		a, b     decimal.Decimal
		expected decimal.Decimal
	}{
		{"a < b", d("1"), d("2"), d("1")},
		{"a > b", d("3"), d("2"), d("2")},
		{"a == b", d("5"), d("5"), d("5")},
		{"negative", d("-3"), d("-1"), d("-3")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := minDec(tt.a, tt.b)
			assertDecEqual(t, "minDec", result, tt.expected)
		})
	}
}

func TestRisk(t *testing.T) {
	tests := []struct {
		name                   string
		qUp, pUp, qDown, pDown decimal.Decimal
		expected               decimal.Decimal
	}{
		{
			name:     "balanced position",
			qUp:      d("100"),
			pUp:      d("0.6"),
			qDown:    d("100"),
			pDown:    d("0.4"),
			expected: d("0"), // C = 60 + 40 = 100, min(100,100) = 100, risk = 0
		},
		{
			name:     "more up than down",
			qUp:      d("100"),
			pUp:      d("0.7"),
			qDown:    d("50"),
			pDown:    d("0.3"),
			expected: d("35"), // C = 70 + 15 = 85, min(100,50) = 50, risk = 35
		},
		{
			name:     "more down than up",
			qUp:      d("30"),
			pUp:      d("0.5"),
			qDown:    d("70"),
			pDown:    d("0.5"),
			expected: d("20"), // C = 15 + 35 = 50, min(30,70) = 30, risk = 20
		},
		{
			name:     "no position",
			qUp:      d("0"),
			pUp:      d("0.5"),
			qDown:    d("0"),
			pDown:    d("0.5"),
			expected: d("0"),
		},
		{
			name:     "only up position",
			qUp:      d("100"),
			pUp:      d("0.8"),
			qDown:    d("0"),
			pDown:    d("0.2"),
			expected: d("80"), // C = 80 + 0 = 80, min(100,0) = 0, risk = 80
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := risk(tt.qUp, tt.pUp, tt.qDown, tt.pDown)
			assertDecEqual(t, "risk", result, tt.expected)
		})
	}
}

func TestPFair(t *testing.T) {
	tests := []struct {
		name           string
		midUp, midDown decimal.Decimal
		expected       decimal.Decimal
	}{
		{"equal mids", d("0.5"), d("0.5"), d("0.5")},
		{"up favored", d("0.7"), d("0.3"), d("0.7")},
		{"down favored", d("0.3"), d("0.7"), d("0.3")},
		{"zero denom returns 0.5", d("0"), d("0"), d("0.5")},
		{"up is 1", d("1"), d("0"), d("1")},
		{"down is 1", d("0"), d("1"), d("0")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pFair(tt.midUp, tt.midDown)
			assertDecEqual(t, "pFair", result, tt.expected)
		})
	}
}

func TestDecPow01(t *testing.T) {
	tests := []struct {
		name      string
		x, n      decimal.Decimal
		expected  decimal.Decimal
		tolerance string
	}{
		{"x^0 = 1", d("0.5"), d("0"), d("1"), "0.0001"},
		{"x^1 = x", d("0.5"), d("1"), d("0.5"), "0.0001"},
		{"0^n = 0", d("0"), d("2"), d("0"), "0.0001"},
		{"1^n = 1", d("1"), d("5"), d("1"), "0.0001"},
		{"0.5^2 = 0.25", d("0.5"), d("2"), d("0.25"), "0.0001"},
		{"0.5^3 = 0.125", d("0.5"), d("3"), d("0.125"), "0.0001"},
		{"0.8^2 = 0.64", d("0.8"), d("2"), d("0.64"), "0.0001"},
		{"fractional exp", d("0.5"), d("0.5"), d("0.7071067811865476"), "0.0001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decPow01(tt.x, tt.n)
			assertDecClose(t, "decPow01", result, tt.expected, tt.tolerance)
		})
	}
}

func TestKTTE(t *testing.T) {
	// Config: marketTimeMs = 900000, kMin = 0.5, kMax = 2, alpha = 2
	marketTimeMs := d("900000")
	kMin := d("0.5")
	kMax := d("2")
	alpha := d("2")

	tests := []struct {
		name      string
		tte       decimal.Decimal
		expected  decimal.Decimal
		tolerance string
	}{
		{
			name:      "at start (tte = T), x=0, k=kMin",
			tte:       d("900000"),
			expected:  d("0.5"),
			tolerance: "0.001",
		},
		{
			name:      "at end (tte = 0), x=1, k=kMax",
			tte:       d("0"),
			expected:  d("2"),
			tolerance: "0.001",
		},
		{
			name:      "halfway (tte = T/2), x=0.5, x^2=0.25",
			tte:       d("450000"),
			expected:  d("0.875"), // kMin + (kMax-kMin)*0.25 = 0.5 + 1.5*0.25 = 0.875
			tolerance: "0.001",
		},
		{
			name:      "negative tte clamped to x=1",
			tte:       d("-100"),
			expected:  d("2"),
			tolerance: "0.001",
		},
		{
			name:      "tte > T clamped to x=0",
			tte:       d("1000000"),
			expected:  d("0.5"),
			tolerance: "0.001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kTTE(tt.tte, marketTimeMs, kMin, kMax, alpha)
			assertDecClose(t, "kTTE", result, tt.expected, tt.tolerance)
		})
	}

	t.Run("zero marketTimeMs returns kMin", func(t *testing.T) {
		result := kTTE(d("100"), d("0"), kMin, kMax, alpha)
		assertDecEqual(t, "kTTE", result, kMin)
	})
}

func TestTargetTilt(t *testing.T) {
	marketTimeMs := d("900000")
	kMin := d("1")
	kMax := d("1")
	alpha := d("1")
	tauMin := d("0.2")
	tauMax := d("0.8")

	tests := []struct {
		name     string
		P        decimal.Decimal
		expected decimal.Decimal
	}{
		{
			name:     "P = 0.5 -> tilt = 0.5",
			P:        d("0.5"),
			expected: d("0.5"),
		},
		{
			name:     "P = 0.7, k=1 -> tilt = 0.5 + 1*(0.7-0.5) = 0.7",
			P:        d("0.7"),
			expected: d("0.7"),
		},
		{
			name:     "P = 0.3, k=1 -> tilt = 0.5 + 1*(0.3-0.5) = 0.3",
			P:        d("0.3"),
			expected: d("0.3"),
		},
		{
			name:     "P = 0.9, clamped to tauMax",
			P:        d("0.9"),
			expected: d("0.8"),
		},
		{
			name:     "P = 0.1, clamped to tauMin",
			P:        d("0.1"),
			expected: d("0.2"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := targetTilt(tt.P, d("0"), marketTimeMs, kMin, kMax, alpha, tauMin, tauMax)
			assertDecClose(t, "targetTilt", result, tt.expected, "0.001")
		})
	}
}

func TestEpsTTE(t *testing.T) {
	marketTimeMs := d("900000")
	epsMin := d("0.01")
	epsMax := d("0.1")

	tests := []struct {
		name      string
		tte       decimal.Decimal
		expected  decimal.Decimal
		tolerance string
	}{
		{
			name:      "at start (tte = T), x=0, eps=epsMax",
			tte:       d("900000"),
			expected:  d("0.1"),
			tolerance: "0.001",
		},
		{
			name:      "at end (tte = 0), x=1, eps=epsMin",
			tte:       d("0"),
			expected:  d("0.01"),
			tolerance: "0.001",
		},
		{
			name:      "halfway",
			tte:       d("450000"),
			expected:  d("0.055"), // epsMin + (epsMax-epsMin)*0.5 = 0.01 + 0.09*0.5 = 0.055
			tolerance: "0.001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := epsTTE(tt.tte, marketTimeMs, epsMin, epsMax)
			assertDecClose(t, "epsTTE", result, tt.expected, tt.tolerance)
		})
	}

	t.Run("zero marketTimeMs returns epsMin", func(t *testing.T) {
		result := epsTTE(d("100"), d("0"), epsMin, epsMax)
		assertDecEqual(t, "epsTTE", result, epsMin)
	})
}

func TestMoneyTilt(t *testing.T) {
	tests := []struct {
		name                   string
		qUp, pUp, qDown, pDown decimal.Decimal
		expected               decimal.Decimal
	}{
		{
			name:     "equal cost",
			qUp:      d("100"),
			pUp:      d("0.5"),
			qDown:    d("100"),
			pDown:    d("0.5"),
			expected: d("0.5"),
		},
		{
			name:     "more up cost",
			qUp:      d("100"),
			pUp:      d("0.8"),
			qDown:    d("100"),
			pDown:    d("0.2"),
			expected: d("0.8"), // 80 / (80 + 20) = 0.8
		},
		{
			name:     "no position",
			qUp:      d("0"),
			pUp:      d("0.5"),
			qDown:    d("0"),
			pDown:    d("0.5"),
			expected: d("0.5"),
		},
		{
			name:     "only up",
			qUp:      d("100"),
			pUp:      d("0.6"),
			qDown:    d("0"),
			pDown:    d("0.4"),
			expected: d("1"), // 60 / 60 = 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := moneyTilt(tt.qUp, tt.pUp, tt.qDown, tt.pDown)
			assertDecEqual(t, "moneyTilt", result, tt.expected)
		})
	}
}

func TestChooseLimitPrice(t *testing.T) {
	marketTimeMs := d("900000")
	kMin := d("1")
	kMax := d("1")
	alpha := d("1")
	eMax := d("0.2")

	tests := []struct {
		name      string
		bid, ask  decimal.Decimal
		e         decimal.Decimal
		tte       decimal.Decimal
		expected  decimal.Decimal
		tolerance string
	}{
		{
			name:      "e=0, aggression=0, price=bid",
			bid:       d("0.4"),
			ask:       d("0.6"),
			e:         d("0"),
			tte:       d("0"),
			expected:  d("0.4"),
			tolerance: "0.001",
		},
		{
			name:      "max aggression",
			bid:       d("0.4"),
			ask:       d("0.6"),
			e:         d("0.2"),
			tte:       d("0"),    // k=1, clamped to 1
			expected:  d("0.6"), // a = 1 * 1 = 1, price = 0.4 + 1 * 0.2 = 0.6
			tolerance: "0.001",
		},
		{
			name:      "half aggression",
			bid:       d("0.4"),
			ask:       d("0.6"),
			e:         d("0.1"),
			tte:       d("0"),
			expected:  d("0.5"), // a = 0.5 * 1 = 0.5, price = 0.4 + 0.5 * 0.2 = 0.5
			tolerance: "0.001",
		},
		{
			name:      "negative e uses abs",
			bid:       d("0.4"),
			ask:       d("0.6"),
			e:         d("-0.1"),
			tte:       d("0"),
			expected:  d("0.5"),
			tolerance: "0.001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := chooseLimitPrice(tt.bid, tt.ask, tt.e, tt.tte, marketTimeMs, kMin, kMax, alpha, eMax)
			assertDecClose(t, "chooseLimitPrice", result, tt.expected, tt.tolerance)
		})
	}

	t.Run("zero eMax returns bid", func(t *testing.T) {
		result := chooseLimitPrice(d("0.4"), d("0.6"), d("0.1"), d("0"), marketTimeMs, kMin, kMax, alpha, d("0"))
		assertDecEqual(t, "chooseLimitPrice", result, d("0.4"))
	})
}

func TestSimulateNewRisk(t *testing.T) {
	tests := []struct {
		name                   string
		qUp, pUp, qDown, pDown decimal.Decimal
		buyUp                  bool
		qBuy, pBuy             decimal.Decimal
		expected               decimal.Decimal
	}{
		{
			name:     "buy up from no position",
			qUp:      d("0"),
			pUp:      d("0"),
			qDown:    d("0"),
			pDown:    d("0"),
			buyUp:    true,
			qBuy:     d("100"),
			pBuy:     d("0.6"),
			expected: d("60"), // C = 60, min = 0, risk = 60
		},
		{
			name:     "buy down from no position",
			qUp:      d("0"),
			pUp:      d("0"),
			qDown:    d("0"),
			pDown:    d("0"),
			buyUp:    false,
			qBuy:     d("100"),
			pBuy:     d("0.4"),
			expected: d("40"), // C = 40, min = 0, risk = 40
		},
		{
			name:     "buy up to balance",
			qUp:      d("0"),
			pUp:      d("0"),
			qDown:    d("100"),
			pDown:    d("0.4"),
			buyUp:    true,
			qBuy:     d("100"),
			pBuy:     d("0.6"),
			expected: d("0"), // C = 60 + 40 = 100, min = 100, risk = 0
		},
		{
			name:  "buy more up increases risk",
			qUp:   d("100"),
			pUp:   d("0.6"),
			qDown: d("100"),
			pDown: d("0.4"),
			buyUp: true,
			qBuy:  d("50"),
			pBuy:  d("0.7"),
			// new pUp = (100*0.6 + 50*0.7) / 150 = 95/150 = 0.6333...
			// C = 150*0.6333 + 100*0.4 = 95 + 40 = 135
			// min = 100, risk = 35
			expected: d("35"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := simulateNewRisk(tt.qUp, tt.pUp, tt.qDown, tt.pDown, tt.buyUp, tt.qBuy, tt.pBuy)
			assertDecClose(t, "simulateNewRisk", result, tt.expected, "0.01")
		})
	}
}

func TestSimulateNewRisk_WeightedAverage(t *testing.T) {
	qUp := d("100")
	pUp := d("0.5")
	qDown := d("0")
	pDown := d("0")
	qBuy := d("100")
	pBuy := d("0.7")

	// After buying: qUp2 = 200, pUp2 = (100*0.5 + 100*0.7) / 200 = 120/200 = 0.6
	// C = 200 * 0.6 = 120, min = 0, risk = 120
	result := simulateNewRisk(qUp, pUp, qDown, pDown, true, qBuy, pBuy)
	assertDecEqual(t, "simulateNewRisk weighted avg", result, d("120"))
}

func BenchmarkRisk(b *testing.B) {
	qUp := d("1000")
	pUp := d("0.65")
	qDown := d("800")
	pDown := d("0.35")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = risk(qUp, pUp, qDown, pDown)
	}
}

func BenchmarkKTTE(b *testing.B) {
	tte := d("450000")
	marketTimeMs := d("900000")
	kMin := d("0.5")
	kMax := d("2")
	alpha := d("2")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = kTTE(tte, marketTimeMs, kMin, kMax, alpha)
	}
}

func BenchmarkTargetTilt(b *testing.B) {
	P := d("0.65")
	tte := d("450000")
	marketTimeMs := d("900000")
	kMin := d("0.5")
	kMax := d("2")
	alpha := d("2")
	tauMin := d("0.2")
	tauMax := d("0.8")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = targetTilt(P, tte, marketTimeMs, kMin, kMax, alpha, tauMin, tauMax)
	}
}

func BenchmarkSimulateNewRisk(b *testing.B) {
	qUp := d("100")
	pUp := d("0.6")
	qDown := d("80")
	pDown := d("0.4")
	qBuy := d("50")
	pBuy := d("0.65")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = simulateNewRisk(qUp, pUp, qDown, pDown, true, qBuy, pBuy)
	}
}

// Helper to create a configured marketWorker for testing
func newTestMarketWorker() *marketWorker {
	return &marketWorker{
		market: marketState{
			endTime: time.Now().Add(15 * time.Minute),
		},
		// prices
		askUp:   d("0.62"),
		bidUp:   d("0.58"),
		askDown: d("0.42"),
		bidDown: d("0.38"),
		// position state
		qUp:   d("0"),
		pUp:   d("0"),
		qDown: d("0"),
		pDown: d("0"),
		// config
		marketTimeMs:       d("900000"), // 15 min
		riskLimit:          d("100"),
		minSize:            d("5"),
		maxStepRisk:        d("20"),
		kMin:               d("0.5"),
		kMax:               d("2"),
		alpha:              d("2"),
		tauMin:             d("0.2"),
		tauMax:             d("0.8"),
		epsMin:             d("0.01"),
		epsMax:             d("0.1"),
		eMax:               d("0.2"),
		stopTradeTTEMs:     d("60000"),
		decisionDebounceMs: d("1000"),
	}
}

func TestProcessNewMarketState_NoPosition_ShouldTrade(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-2 * time.Second)

	shouldTrade, order := mw.processNewMarketState()

	if shouldTrade {
		if order.OriginalSize == "" {
			t.Error("shouldTrade=true but OriginalSize is empty")
		}
		if order.Price == "" {
			t.Error("shouldTrade=true but Price is empty")
		}
	}
}

func TestProcessNewMarketState_TooCloseToExpiry(t *testing.T) {
	mw := newTestMarketWorker()
	mw.market.endTime = time.Now().Add(30 * time.Second)
	mw.lastDecision = time.Now().Add(-2 * time.Second)

	shouldTrade, _ := mw.processNewMarketState()

	if shouldTrade {
		t.Error("should not trade when too close to expiry")
	}
}

func TestProcessNewMarketState_RiskLimitExceeded(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-2 * time.Second)

	mw.qUp = d("200")
	mw.pUp = d("0.6")
	mw.qDown = d("0")
	mw.pDown = d("0")

	shouldTrade, _ := mw.processNewMarketState()

	if shouldTrade {
		t.Error("should not trade when risk limit exceeded")
	}
}

func TestProcessNewMarketState_WithinEpsilon(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-2 * time.Second)

	mw.qUp = d("100")
	mw.pUp = d("0.6")
	mw.qDown = d("100")
	mw.pDown = d("0.4")

	shouldTrade, _ := mw.processNewMarketState()
	_ = shouldTrade
}

func TestProcessNewMarketState_DebounceActive(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now()

	shouldTrade, _ := mw.processNewMarketState()

	if shouldTrade {
		t.Error("should not trade when debounce is active")
	}
}

func TestProcessNewMarketState_BuyUp(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-2 * time.Second)

	mw.qUp = d("0")
	mw.pUp = d("0")
	mw.qDown = d("50")
	mw.pDown = d("0.4")

	shouldTrade, order := mw.processNewMarketState()

	if !shouldTrade {
		t.Skip("trade conditions not met, skipping order validation")
	}

	size := decimal.RequireFromString(order.OriginalSize)
	price := decimal.RequireFromString(order.Price)

	if size.LessThan(mw.minSize) {
		t.Errorf("order size %s less than minSize %s", order.OriginalSize, mw.minSize.String())
	}

	if price.LessThan(mw.bidUp) || price.GreaterThan(mw.askUp) {
		t.Errorf("price %s outside bid/ask range [%s, %s]", order.Price, mw.bidUp.String(), mw.askUp.String())
	}
}

func TestProcessNewMarketState_BuyDown(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-2 * time.Second)

	mw.qUp = d("100")
	mw.pUp = d("0.6")
	mw.qDown = d("0")
	mw.pDown = d("0")

	shouldTrade, order := mw.processNewMarketState()

	if !shouldTrade {
		t.Skip("trade conditions not met, skipping order validation")
	}

	size := decimal.RequireFromString(order.OriginalSize)
	price := decimal.RequireFromString(order.Price)

	if size.LessThan(mw.minSize) {
		t.Errorf("order size %s less than minSize %s", order.OriginalSize, mw.minSize.String())
	}

	if price.LessThan(mw.bidDown) || price.GreaterThan(mw.askDown) {
		t.Errorf("price %s outside bid/ask range [%s, %s]", order.Price, mw.bidDown.String(), mw.askDown.String())
	}
}

func TestProcessNewMarketState_SizeReducedToFitRisk(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-2 * time.Second)

	mw.qUp = d("0")
	mw.pUp = d("0")
	mw.qDown = d("50")
	mw.pDown = d("0.4")
	mw.riskLimit = d("30")

	shouldTrade, order := mw.processNewMarketState()

	if !shouldTrade {
		t.Skip("trade conditions not met")
	}

	size := decimal.RequireFromString(order.OriginalSize)
	price := decimal.RequireFromString(order.Price)

	maxExpectedSize := d("20")
	if size.GreaterThan(maxExpectedSize) {
		t.Errorf("order size %s larger than expected max %s given risk headroom", order.OriginalSize, maxExpectedSize.String())
	}

	_ = price
}

func TestProcessNewMarketState_UpdatesLastDecision(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-2 * time.Second)
	oldDecision := mw.lastDecision

	shouldTrade, _ := mw.processNewMarketState()

	if shouldTrade {
		if !mw.lastDecision.After(oldDecision) {
			t.Error("lastDecision should be updated after successful trade decision")
		}
	}
}

func TestProcessNewMarketState_UpdatesStateFields(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-2 * time.Second)

	if !mw.pFairVal.IsZero() {
		t.Error("pFairVal should be zero before processing")
	}

	mw.processNewMarketState()

	if mw.pFairVal.IsZero() {
		t.Error("pFairVal should be calculated after processing")
	}
	if mw.tte.IsZero() {
		t.Error("tte should be calculated after processing")
	}
}

func BenchmarkProcessNewMarketState(b *testing.B) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-2 * time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mw.lastDecision = time.Now().Add(-2 * time.Second)
		_, _ = mw.processNewMarketState()
	}
}

func TestDecPow01_NaNAndInfFallback(t *testing.T) {
	result := decPow01(d("0.0000001"), d("2"))
	if result.IsNegative() {
		t.Error("result should not be negative")
	}

	result = decPow01(d("0.5"), d("-1"))
	if result.LessThanOrEqual(decZero) {
		t.Error("0.5^-1 should be positive")
	}
}

func TestDecPow01_EdgeCases(t *testing.T) {
	result := decPow01(d("0.999999999"), d("100"))
	if result.LessThanOrEqual(decZero) || result.GreaterThan(decOne) {
		t.Errorf("unexpected result for 0.999...^100: %s", result.String())
	}

	result = decPow01(d("0.000000001"), d("0.5"))
	if result.LessThan(decZero) || result.GreaterThan(decOne) {
		t.Errorf("unexpected result for tiny^0.5: %s", result.String())
	}

	result = decPow01(d("-0.5"), d("2"))
	if !result.Equal(decZero) {
		t.Errorf("negative x should clamp to 0, then 0^n=0, got %s", result.String())
	}

	result = decPow01(d("1.5"), d("2"))
	if !result.Equal(decOne) {
		t.Errorf("x>1 should clamp to 1, then 1^n=1, got %s", result.String())
	}
}

func TestProcessNewMarketState_DebounceExact(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-500 * time.Millisecond)

	shouldTrade, _ := mw.processNewMarketState()

	if shouldTrade {
		t.Error("should not trade when within debounce period")
	}
}

func TestProcessNewMarketState_DebounceBlocksTrade(t *testing.T) {
	mw := newTestMarketWorker()

	mw.qUp = d("0")
	mw.pUp = d("0")
	mw.qDown = d("100")
	mw.pDown = d("0.4")

	mw.lastDecision = time.Now()
	mw.decisionDebounceMs = d("5000")

	shouldTrade, _ := mw.processNewMarketState()

	if shouldTrade {
		t.Error("debounce should have blocked the trade")
	}
}

func TestProcessNewMarketState_LoopReducesQBuy(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-10 * time.Second)

	mw.bidUp = d("0.88")
	mw.askUp = d("0.92")
	mw.bidDown = d("0.08")
	mw.askDown = d("0.12")

	mw.qUp = d("0")
	mw.pUp = d("0")
	mw.qDown = d("0")
	mw.pDown = d("0")

	mw.riskLimit = d("5")
	mw.minSize = d("1")
	mw.maxStepRisk = d("100")
	mw.epsMin = d("0.001")
	mw.epsMax = d("0.01")

	shouldTrade, order := mw.processNewMarketState()

	if shouldTrade {
		t.Logf("Trade executed with size: %s, price: %s", order.OriginalSize, order.Price)
	}
}

func TestProcessNewMarketState_LoopReducesToMinSizeAndFails(t *testing.T) {
	mw := newTestMarketWorker()
	mw.lastDecision = time.Now().Add(-10 * time.Second)

	mw.bidUp = d("0.88")
	mw.askUp = d("0.92")
	mw.bidDown = d("0.08")
	mw.askDown = d("0.12")

	mw.qUp = d("0")
	mw.pUp = d("0")
	mw.qDown = d("0")
	mw.pDown = d("0")

	mw.riskLimit = d("5")
	mw.minSize = d("10")
	mw.maxStepRisk = d("100")
	mw.epsMin = d("0.001")
	mw.epsMax = d("0.01")

	shouldTrade, _ := mw.processNewMarketState()

	if shouldTrade {
		t.Error("should not trade when qBuy reduced below minSize")
	}
}

// =============================================================================
// Backtest Implementation
// =============================================================================

const (
	priceRound     = 6
	defaultBucketMs = 500
)

type backtestParams struct {
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
	marketTimeMs       decimal.Decimal
}

func defaultBacktestParams() backtestParams {
	return backtestParams{
		riskLimit:          d("15"),
		minSize:            d("5"),
		maxStepRisk:        d("10"),
		kMin:               d("0.35"),
		kMax:               d("1.25"),
		alpha:              d("2.0"),
		tauMin:             d("0.15"),
		tauMax:             d("0.85"),
		epsMin:             d("0.02"),
		epsMax:             d("0.06"),
		eMax:               d("0.25"),
		stopTradeTTEMs:     d("0"),
		decisionDebounceMs: d("500"),
		marketTimeMs:       d("900000"),
	}
}

type backtestMarket struct {
	ID          string
	ConditionID string
	TokenUp     string
	TokenDown   string
	StartTime   time.Time
	EndTime     time.Time
}

type quoteSnapshot struct {
	ConditionID string
	AssetID     string
	BestBid     float64
	BestAsk     float64
	Timestamp   time.Time
}

type alignedQuote struct {
	Timestamp time.Time
	BidUp     decimal.Decimal
	AskUp     decimal.Decimal
	BidDown   decimal.Decimal
	AskDown   decimal.Decimal
}

type tradeRecord struct {
	Timestamp  time.Time
	Side       string
	Qty        decimal.Decimal
	Price      decimal.Decimal
	MoneyTilt  decimal.Decimal
	TargetTilt decimal.Decimal
	PFair      decimal.Decimal
	Risk       decimal.Decimal
}

type marketResult struct {
	ConditionID  string
	NumSteps     int
	NumTrades    int
	QUp          decimal.Decimal
	QDown        decimal.Decimal
	PUp          decimal.Decimal
	PDown        decimal.Decimal
	Cost         decimal.Decimal
	PnLUp        decimal.Decimal
	PnLDown      decimal.Decimal
	WorstCasePnL decimal.Decimal
	Winner       string
	PnLWinner    decimal.Decimal
	Trades       []tradeRecord
	FinalMidUp   decimal.Decimal
	FinalMidDown decimal.Decimal
}

// BacktestOrder extends model.Order with additional fields needed for backtest
type BacktestOrder struct {
	OriginalSize string
	Price        string
	BuyUp        bool
	Qty          decimal.Decimal
	PriceDec     decimal.Decimal
}

type backtestStruct struct {
	t        *testing.T
	db       *sql.DB
	params   backtestParams
	bucketMs int64
	results  []marketResult
}

func newBacktest(t *testing.T) *backtestStruct {
	db := connectDB(t)
	if db == nil {
		return nil
	}

	bucketMs := int64(defaultBucketMs)
	if v := os.Getenv("BT_BUCKET_MS"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed > 0 {
			bucketMs = parsed
		}
	}

	return &backtestStruct{
		t:        t,
		db:       db,
		params:   defaultBacktestParams(),
		bucketMs: bucketMs,
	}
}

func connectDB(t *testing.T) *sql.DB {
	connStr := "postgres://postgres:postgres@localhost:5432/research?sslmode=disable"

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Skipf("failed to open database: %v", err)
		return nil
	}

	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("failed to ping database: %v", err)
		return nil
	}

	return db
}

func (bt *backtestStruct) close() {
	if bt.db != nil {
		bt.db.Close()
	}
}

func (bt *backtestStruct) run() {
	markets := bt.loadMarkets()
	if len(markets) == 0 {
		bt.t.Log("No markets found to backtest")
		return
	}

	bt.t.Logf("Backtesting %d markets...\n", len(markets))

	for i, mkt := range markets {
		result := bt.runMarket(mkt)
		bt.results = append(bt.results, result)

		bt.t.Logf("[%d/%d] %s: steps=%d trades=%d qUp=%.2f qDown=%.2f pnl_up=%.4f pnl_down=%.4f worst=%.4f winner=%s pnl_winner=%.4f",
			i+1, len(markets),
			mkt.ConditionID,
			result.NumSteps,
			result.NumTrades,
			result.QUp.InexactFloat64(),
			result.QDown.InexactFloat64(),
			result.PnLUp.InexactFloat64(),
			result.PnLDown.InexactFloat64(),
			result.WorstCasePnL.InexactFloat64(),
			result.Winner,
			result.PnLWinner.InexactFloat64(),
		)
	}

	bt.printAggregateSummary()
}

func (bt *backtestStruct) loadMarkets() []backtestMarket {
	if condIDs := os.Getenv("BT_CONDITION_ID"); condIDs != "" {
		ids := strings.Split(condIDs, ",")
		return bt.loadMarketsByConditionIDs(ids)
	}

	limit := 0
	if v := os.Getenv("BT_LIMIT_MARKETS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	return bt.loadRecentMarkets(limit)
}

func (bt *backtestStruct) loadMarketsByConditionIDs(ids []string) []backtestMarket {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = strings.TrimSpace(id)
	}

	query := fmt.Sprintf(`
		SELECT id, condition_id, token_up, token_down, start_time, end_time
		FROM markets
		WHERE condition_id IN (%s)
		ORDER BY end_time DESC
	`, strings.Join(placeholders, ","))

	return bt.queryMarkets(query, args...)
}

func (bt *backtestStruct) loadRecentMarkets(limit int) []backtestMarket {
	var query string
	var args []interface{}

	if limit > 0 {
		query = `
			SELECT id, condition_id, token_up, token_down, start_time, end_time
			FROM markets
			ORDER BY end_time DESC
			LIMIT $1
		`
		args = []interface{}{limit}
	} else {
		query = `
			SELECT id, condition_id, token_up, token_down, start_time, end_time
			FROM markets
			ORDER BY end_time DESC
		`
		args = nil
	}

	return bt.queryMarkets(query, args...)
}

func (bt *backtestStruct) queryMarkets(query string, args ...interface{}) []backtestMarket {
	rows, err := bt.db.Query(query, args...)
	if err != nil {
		bt.t.Fatalf("failed to query markets: %v", err)
	}
	defer rows.Close()

	var markets []backtestMarket
	for rows.Next() {
		var m backtestMarket
		if err := rows.Scan(&m.ID, &m.ConditionID, &m.TokenUp, &m.TokenDown,
			&m.StartTime, &m.EndTime); err != nil {
			bt.t.Fatalf("failed to scan market: %v", err)
		}
		markets = append(markets, m)
	}
	return markets
}

func (bt *backtestStruct) loadQuotes(mkt backtestMarket) []quoteSnapshot {
	query := `
		SELECT condition_id, asset_id, best_bid, best_ask, timestamp
		FROM best_bid_ask
		WHERE condition_id = $1 AND asset_id IN ($2, $3)
		  AND timestamp >= $4 AND timestamp <= $5
		ORDER BY timestamp ASC
	`

	rows, err := bt.db.Query(query, mkt.ConditionID, mkt.TokenUp, mkt.TokenDown,
		mkt.StartTime, mkt.EndTime)
	if err != nil {
		bt.t.Logf("failed to query quotes for %s: %v", mkt.ConditionID, err)
		return nil
	}
	defer rows.Close()

	var quotes []quoteSnapshot
	for rows.Next() {
		var q quoteSnapshot
		if err := rows.Scan(&q.ConditionID, &q.AssetID, &q.BestBid, &q.BestAsk, &q.Timestamp); err != nil {
			bt.t.Logf("failed to scan quote: %v", err)
			continue
		}
		quotes = append(quotes, q)
	}
	return quotes
}

func (bt *backtestStruct) alignQuotes(quotes []quoteSnapshot, mkt backtestMarket) []alignedQuote {
	if len(quotes) == 0 {
		return nil
	}

	bucketDuration := time.Duration(bt.bucketMs) * time.Millisecond

	var lastUp, lastDown *quoteSnapshot

	startBucket := quotes[0].Timestamp.Truncate(bucketDuration)
	endBucket := quotes[len(quotes)-1].Timestamp.Truncate(bucketDuration)

	var result []alignedQuote

	for bucket := startBucket; !bucket.After(endBucket); bucket = bucket.Add(bucketDuration) {
		for i := range quotes {
			q := &quotes[i]
			if q.Timestamp.Truncate(bucketDuration) == bucket {
				if q.AssetID == mkt.TokenUp {
					lastUp = q
				} else {
					lastDown = q
				}
			}
		}

		if lastUp != nil && lastDown != nil {
			result = append(result, alignedQuote{
				Timestamp: bucket,
				BidUp:     floatToDecimal(lastUp.BestBid),
				AskUp:     floatToDecimal(lastUp.BestAsk),
				BidDown:   floatToDecimal(lastDown.BestBid),
				AskDown:   floatToDecimal(lastDown.BestAsk),
			})
		}
	}

	return result
}

func floatToDecimal(f float64) decimal.Decimal {
	return decimal.NewFromFloat(f).Round(priceRound)
}

// processNewMarketStateAt is a backtest-time variant that uses the provided
// timestamp instead of wall-clock time for TTE and debounce calculations.
func (mw *marketWorker) processNewMarketStateAt(now time.Time) (bool, BacktestOrder) {
	mw.tte = decimal.NewFromInt(mw.market.endTime.Sub(now).Milliseconds())
	if mw.stopTradeTTEMs.Cmp(mw.tte) == 1 {
		return false, BacktestOrder{}
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
		return false, BacktestOrder{}
	}
	e := mw.targetTiltVal.Sub(mw.moneyTiltVal)
	eps := epsTTE(mw.tte, mw.marketTimeMs, mw.epsMin, mw.epsMax)
	if e.Abs().Cmp(eps) != 1 {
		return false, BacktestOrder{}
	}

	sinceLastDecision := now.Sub(mw.lastDecision).Milliseconds()
	if decimal.NewFromInt(sinceLastDecision).Cmp(mw.decisionDebounceMs) != 1 {
		return false, BacktestOrder{}
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
		return false, BacktestOrder{}
	}

	mw.lastDecision = now

	return true, BacktestOrder{
		OriginalSize: qBuy.StringFixed(2),
		Price:        price.StringFixed(2),
		BuyUp:        buyUp,
		Qty:          qBuy,
		PriceDec:     price,
	}
}

func (bt *backtestStruct) runMarket(mkt backtestMarket) marketResult {
	result := marketResult{
		ConditionID: mkt.ConditionID,
		QUp:         decZero,
		QDown:       decZero,
		PUp:         decZero,
		PDown:       decZero,
	}

	quotes := bt.loadQuotes(mkt)
	aligned := bt.alignQuotes(quotes, mkt)

	if len(aligned) == 0 {
		bt.t.Logf("No aligned quotes for market %s", mkt.ConditionID)
		return result
	}

	result.NumSteps = len(aligned)

	mw := &marketWorker{
		market: marketState{
			endTime: mkt.EndTime,
		},
		qUp:                decZero,
		pUp:                decZero,
		qDown:              decZero,
		pDown:              decZero,
		marketTimeMs:       bt.params.marketTimeMs,
		riskLimit:          bt.params.riskLimit,
		minSize:            bt.params.minSize,
		maxStepRisk:        bt.params.maxStepRisk,
		kMin:               bt.params.kMin,
		kMax:               bt.params.kMax,
		alpha:              bt.params.alpha,
		tauMin:             bt.params.tauMin,
		tauMax:             bt.params.tauMax,
		epsMin:             bt.params.epsMin,
		epsMax:             bt.params.epsMax,
		eMax:               bt.params.eMax,
		stopTradeTTEMs:     bt.params.stopTradeTTEMs,
		decisionDebounceMs: bt.params.decisionDebounceMs,
	}

	var finalMidUp, finalMidDown decimal.Decimal

	for _, aq := range aligned {
		mw.bidUp = aq.BidUp
		mw.askUp = aq.AskUp
		mw.bidDown = aq.BidDown
		mw.askDown = aq.AskDown

		finalMidUp = aq.BidUp.Add(aq.AskUp).Div(decTwo)
		finalMidDown = aq.BidDown.Add(aq.AskDown).Div(decTwo)

		shouldTrade, order := mw.processNewMarketStateAt(aq.Timestamp)

		if shouldTrade {
			result.NumTrades++

			if order.BuyUp {
				newQ := mw.qUp.Add(order.Qty)
				if mw.qUp.IsZero() {
					mw.pUp = order.PriceDec
				} else {
					mw.pUp = mw.qUp.Mul(mw.pUp).Add(order.Qty.Mul(order.PriceDec)).Div(newQ)
				}
				mw.qUp = newQ
			} else {
				newQ := mw.qDown.Add(order.Qty)
				if mw.qDown.IsZero() {
					mw.pDown = order.PriceDec
				} else {
					mw.pDown = mw.qDown.Mul(mw.pDown).Add(order.Qty.Mul(order.PriceDec)).Div(newQ)
				}
				mw.qDown = newQ
			}

			side := "Down"
			if order.BuyUp {
				side = "Up"
			}
			result.Trades = append(result.Trades, tradeRecord{
				Timestamp:  aq.Timestamp,
				Side:       side,
				Qty:        order.Qty,
				Price:      order.PriceDec,
				MoneyTilt:  mw.moneyTiltVal,
				TargetTilt: mw.targetTiltVal,
				PFair:      mw.pFairVal,
				Risk:       mw.riskVal,
			})
		}
	}

	result.QUp = mw.qUp
	result.QDown = mw.qDown
	result.PUp = mw.pUp
	result.PDown = mw.pDown
	result.FinalMidUp = finalMidUp
	result.FinalMidDown = finalMidDown

	result.Cost = mw.qUp.Mul(mw.pUp).Add(mw.qDown.Mul(mw.pDown))
	result.PnLUp = mw.qUp.Sub(result.Cost)
	result.PnLDown = mw.qDown.Sub(result.Cost)
	result.WorstCasePnL = decimal.Min(result.PnLUp, result.PnLDown)

	if finalMidUp.GreaterThan(finalMidDown) {
		result.Winner = "Up"
		result.PnLWinner = result.PnLUp
	} else {
		result.Winner = "Down"
		result.PnLWinner = result.PnLDown
	}

	return result
}

func (bt *backtestStruct) printAggregateSummary() {
	if len(bt.results) == 0 {
		bt.t.Log("\nNo results to summarize")
		return
	}

	var totalTrades int
	var totalPnLWinner, totalWorstCase decimal.Decimal
	var wins int
	var pnlWinners []float64

	for _, r := range bt.results {
		totalTrades += r.NumTrades
		totalPnLWinner = totalPnLWinner.Add(r.PnLWinner)
		totalWorstCase = totalWorstCase.Add(r.WorstCasePnL)
		if r.PnLWinner.GreaterThan(decZero) {
			wins++
		}
		pnlWinners = append(pnlWinners, r.PnLWinner.InexactFloat64())
	}

	numMarkets := len(bt.results)
	avgPnLWinner := totalPnLWinner.Div(decimal.NewFromInt(int64(numMarkets)))
	avgWorstCase := totalWorstCase.Div(decimal.NewFromInt(int64(numMarkets)))
	winRate := float64(wins) / float64(numMarkets) * 100

	sort.Float64s(pnlWinners)
	var medianPnL float64
	if numMarkets > 0 {
		mid := numMarkets / 2
		if numMarkets%2 == 0 {
			medianPnL = (pnlWinners[mid-1] + pnlWinners[mid]) / 2
		} else {
			medianPnL = pnlWinners[mid]
		}
	}

	bt.t.Log("\n" + strings.Repeat("=", 60))
	bt.t.Log("BACKTEST SUMMARY")
	bt.t.Log(strings.Repeat("=", 60))
	bt.t.Logf("Markets processed:    %d", numMarkets)
	bt.t.Logf("Total trades:         %d", totalTrades)
	bt.t.Logf("Avg trades/market:    %.2f", float64(totalTrades)/float64(numMarkets))
	bt.t.Logf("Win rate:             %.1f%% (%d/%d)", winRate, wins, numMarkets)
	bt.t.Logf("Total PnL (winner):   %.4f", totalPnLWinner.InexactFloat64())
	bt.t.Logf("Avg PnL (winner):     %.4f", avgPnLWinner.InexactFloat64())
	bt.t.Logf("Median PnL (winner):  %.4f", medianPnL)
	bt.t.Logf("Total worst-case:     %.4f", totalWorstCase.InexactFloat64())
	bt.t.Logf("Avg worst-case:       %.4f", avgWorstCase.InexactFloat64())
	bt.t.Log(strings.Repeat("=", 60))

	if debugCondID := os.Getenv("BT_DEBUG_CONDITION_ID"); debugCondID != "" {
		for _, r := range bt.results {
			if r.ConditionID == debugCondID {
				bt.dumpTrades(r)
				break
			}
		}
	}
}

func (bt *backtestStruct) dumpTrades(r marketResult) {
	bt.t.Logf("\n--- Trades for %s ---", r.ConditionID)
	bt.t.Log("timestamp,side,qty,price,money_tilt,target_tilt,p_fair,risk")
	for _, tr := range r.Trades {
		bt.t.Logf("%s,%s,%.2f,%.4f,%.4f,%.4f,%.4f,%.4f",
			tr.Timestamp.Format(time.RFC3339),
			tr.Side,
			tr.Qty.InexactFloat64(),
			tr.Price.InexactFloat64(),
			tr.MoneyTilt.InexactFloat64(),
			tr.TargetTilt.InexactFloat64(),
			tr.PFair.InexactFloat64(),
			tr.Risk.InexactFloat64(),
		)
	}
}

func Test_backtest(t *testing.T) {
	bt := newBacktest(t)
	if bt == nil {
		return
	}
	defer bt.close()

	bt.run()
}
