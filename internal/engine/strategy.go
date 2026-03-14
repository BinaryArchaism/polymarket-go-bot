package engine

import (
	"math"

	"github.com/shopspring/decimal"
)

var (
	decZero = decimal.Zero
	decOne  = decimal.NewFromInt(1)
	decTwo  = decimal.NewFromInt(2)

	// avoid NewFromFloat for determinism
	decHalf    = decimal.NewFromInt(1).Div(decimal.NewFromInt(2)) // 0.5
	decOneHalf = decimal.NewFromInt(3).Div(decimal.NewFromInt(2)) // 1.5
)

// clamp restricts x to the range [a, b]
func clamp(x, a, b decimal.Decimal) decimal.Decimal {
	if x.LessThan(a) {
		return a
	}
	if x.GreaterThan(b) {
		return b
	}
	return x
}

func minDec(a, b decimal.Decimal) decimal.Decimal {
	if a.LessThan(b) {
		return a
	}
	return b
}

// risk calculates position risk as total cost minus hedged quantity
// C = qUp * pUp + qDown * pDown; risk = C - min(qUp, qDown)
//
// Assumptions:
// - qUp/qDown are contract quantities (payoff=$1 per contract).
// - pUp/pDown are prices in USD per contract (0..1).
func risk(qUp, pUp, qDown, pDown decimal.Decimal) decimal.Decimal {
	C := qUp.Mul(pUp).Add(qDown.Mul(pDown))
	return C.Sub(minDec(qUp, qDown))
}

// pFair calculates fair probability from mid prices
// P = midUp / (midUp + midDown)
func pFair(midUp, midDown decimal.Decimal) decimal.Decimal {
	denom := midUp.Add(midDown)
	if denom.LessThanOrEqual(decZero) {
		return decHalf
	}
	return midUp.Div(denom)
}

// decPow01 computes x^n using float64, intended for x in [0,1].
// This is stable for your use-case (x is clamped and no overflow risk).
func decPow01(x, n decimal.Decimal) decimal.Decimal {
	// clamp x to [0,1]
	x = clamp(x, decZero, decOne)

	// fast paths
	if n.IsZero() {
		return decOne
	}
	if x.IsZero() {
		return decZero
	}
	if x.Equal(decOne) {
		return decOne
	}

	xf, _ := x.Float64()
	nf, _ := n.Float64()

	return decimal.NewFromFloat(math.Pow(xf, nf))
}

// kTTE calculates k based on time-to-expiry
// k = k_min + (k_max - k_min) * x^alpha, where x = clamp(1 - tte/T, 0, 1)
func kTTE(tte, marketTimeMs, kMin, kMax, alpha decimal.Decimal) decimal.Decimal {
	// Guard: avoid div-by-zero / negative time horizon
	if marketTimeMs.LessThanOrEqual(decZero) {
		return kMin
	}

	// x = clamp(1 - tte/T, 0, 1)
	x := clamp(decOne.Sub(tte.Div(marketTimeMs)), decZero, decOne)

	// x^alpha
	xPow := decPow01(x, alpha)

	return kMin.Add(kMax.Sub(kMin).Mul(xPow))
}

// targetTilt calculates target tilt based on fair price and TTE
// target = clamp(0.5 + k * (P - 0.5), tau_min, tau_max)
func targetTilt(P, tte, marketTimeMs, kMin, kMax, alpha, tauMin, tauMax decimal.Decimal) decimal.Decimal {
	k := kTTE(tte, marketTimeMs, kMin, kMax, alpha)
	tilt := decHalf.Add(k.Mul(P.Sub(decHalf)))
	return clamp(tilt, tauMin, tauMax)
}

// epsTTE calculates epsilon based on time-to-expiry
// eps = eps_min + (eps_max - eps_min) * (1 - x), where x = clamp(1 - tte/T, 0, 1)
// bigger eps early, smaller late
func epsTTE(tte, marketTimeMs, epsMin, epsMax decimal.Decimal) decimal.Decimal {
	if marketTimeMs.LessThanOrEqual(decZero) {
		return epsMin
	}

	x := clamp(decOne.Sub(tte.Div(marketTimeMs)), decZero, decOne)
	oneMinusX := decOne.Sub(x)
	return epsMin.Add(epsMax.Sub(epsMin).Mul(oneMinusX))
}

// moneyTilt calculates the money tilt based on positions
// tilt = Cy / (Cy + Cn), where Cy = qUp * pUp_avg, Cn = qDown * pDown_avg
func moneyTilt(qUp, pUp, qDown, pDown decimal.Decimal) decimal.Decimal {
	cUp := qUp.Mul(pUp)
	cDown := qDown.Mul(pDown)
	denom := cUp.Add(cDown)
	if denom.IsZero() {
		return decHalf
	}
	return cUp.Div(denom)
}

// chooseLimitPrice calculates limit price based on aggression
// a = clamp(|e| / e_max, 0, 1) * clamp(k_of_tte, 0, 1.5)
// price = bid + a * (ask - bid)
func chooseLimitPrice(bid, ask, e, tte, marketTimeMs, kMin, kMax, alpha, eMax decimal.Decimal) decimal.Decimal {
	// Guard: avoid division by 0 for eMax
	if eMax.LessThanOrEqual(decZero) {
		return bid
	}

	absE := e.Abs()

	// clamp(|e| / e_max, 0, 1)
	aggression1 := clamp(absE.Div(eMax), decZero, decOne)

	// clamp(k_of_tte, 0, 1.5)
	aggression2 := clamp(kTTE(tte, marketTimeMs, kMin, kMax, alpha), decZero, decOneHalf)

	// a = aggression1 * aggression2
	a := aggression1.Mul(aggression2)

	return bid.Add(a.Mul(ask.Sub(bid)))
}

// simulateNewRisk calculates the new risk if we were to buy
// py_avg_new = (qy * py_avg + q_buy * p_buy) / (qy + q_buy)
func simulateNewRisk(qUp, pUp, qDown, pDown decimal.Decimal, buyUp bool, qBuy, pBuy decimal.Decimal) decimal.Decimal {
	if buyUp {
		qUp2 := qUp.Add(qBuy)
		pUp2 := pUp
		if !qUp2.IsZero() {
			pUp2 = qUp.Mul(pUp).Add(qBuy.Mul(pBuy)).Div(qUp2)
		}
		return risk(qUp2, pUp2, qDown, pDown)
	}

	qDown2 := qDown.Add(qBuy)
	pDown2 := pDown
	if !qDown2.IsZero() {
		pDown2 = qDown.Mul(pDown).Add(qBuy.Mul(pBuy)).Div(qDown2)
	}
	return risk(qUp, pUp, qDown2, pDown2)
}
