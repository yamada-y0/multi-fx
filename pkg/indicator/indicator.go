package indicator

import (
	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/pkg/market"
)

const precision = 5 // 小数点以下桁数

// Result はローソク足スライス（新しい順）から算出したテクニカル指標
type Result struct {
	SMA  map[int]decimal.Decimal `json:"SMA"`  // 期間 → 値
	EMA  map[int]decimal.Decimal `json:"EMA"`  // 期間 → 値
	RSI  decimal.Decimal         `json:"RSI"`  // 14期間
	ATR  decimal.Decimal         `json:"ATR"`  // 14期間
	MACD MACDResult              `json:"MACD"`
}

type MACDResult struct {
	MACD   decimal.Decimal `json:"MACD"`
	Signal decimal.Decimal `json:"Signal"`
	Hist   decimal.Decimal `json:"Hist"`
}

func round(d decimal.Decimal) decimal.Decimal {
	return d.Round(precision)
}

// Calc はローソク足スライス（新しい順）からテクニカル指標を算出する。
// データが不足している指標はゼロ値になる。
func Calc(candles []market.Candle) Result {
	// 計算は古い順で行うため反転
	n := len(candles)
	closes := make([]decimal.Decimal, n)
	highs := make([]decimal.Decimal, n)
	lows := make([]decimal.Decimal, n)
	for i, c := range candles {
		closes[n-1-i] = c.Close
		highs[n-1-i] = c.High
		lows[n-1-i] = c.Low
	}

	smaPeriods := []int{20, 50, 200}
	emaPeriods := []int{12, 26}

	sma := calcSMAMap(closes, smaPeriods)
	for k, v := range sma {
		sma[k] = round(v)
	}
	ema := calcEMAMap(closes, emaPeriods)
	for k, v := range ema {
		ema[k] = round(v)
	}
	macd := calcMACD(closes)

	return Result{
		SMA:  sma,
		EMA:  ema,
		RSI:  round(calcRSI(closes, 14)),
		ATR:  round(calcATR(highs, lows, closes, 14)),
		MACD: MACDResult{MACD: round(macd.MACD), Signal: round(macd.Signal), Hist: round(macd.Hist)},
	}
}

// --- SMA ---

func calcSMA(closes []decimal.Decimal, period int) decimal.Decimal {
	if len(closes) < period {
		return decimal.Zero
	}
	src := closes[len(closes)-period:]
	sum := decimal.Zero
	for _, v := range src {
		sum = sum.Add(v)
	}
	return sum.Div(decimal.NewFromInt(int64(period)))
}

func calcSMAMap(closes []decimal.Decimal, periods []int) map[int]decimal.Decimal {
	m := make(map[int]decimal.Decimal, len(periods))
	for _, p := range periods {
		m[p] = calcSMA(closes, p)
	}
	return m
}

// --- EMA ---

func calcEMA(closes []decimal.Decimal, period int) decimal.Decimal {
	if len(closes) < period {
		return decimal.Zero
	}
	k := decimal.NewFromFloat(2.0 / float64(period+1))
	one := decimal.NewFromInt(1)

	// 最初の EMA は SMA で初期化
	sum := decimal.Zero
	for _, v := range closes[:period] {
		sum = sum.Add(v)
	}
	ema := sum.Div(decimal.NewFromInt(int64(period)))

	for _, v := range closes[period:] {
		ema = v.Mul(k).Add(ema.Mul(one.Sub(k)))
	}
	return ema
}

func calcEMAMap(closes []decimal.Decimal, periods []int) map[int]decimal.Decimal {
	m := make(map[int]decimal.Decimal, len(periods))
	for _, p := range periods {
		m[p] = calcEMA(closes, p)
	}
	return m
}

// --- RSI ---

func calcRSI(closes []decimal.Decimal, period int) decimal.Decimal {
	if len(closes) < period+1 {
		return decimal.Zero
	}

	gains := decimal.Zero
	losses := decimal.Zero
	for i := len(closes) - period; i < len(closes); i++ {
		diff := closes[i].Sub(closes[i-1])
		if diff.IsPositive() {
			gains = gains.Add(diff)
		} else {
			losses = losses.Add(diff.Abs())
		}
	}

	p := decimal.NewFromInt(int64(period))
	avgGain := gains.Div(p)
	avgLoss := losses.Div(p)

	if avgLoss.IsZero() {
		return decimal.NewFromInt(100)
	}
	rs := avgGain.Div(avgLoss)
	return decimal.NewFromInt(100).Sub(
		decimal.NewFromInt(100).Div(decimal.NewFromInt(1).Add(rs)),
	)
}

// --- ATR ---

func calcATR(highs, lows, closes []decimal.Decimal, period int) decimal.Decimal {
	if len(closes) < period+1 {
		return decimal.Zero
	}

	trs := make([]decimal.Decimal, 0, period)
	for i := len(closes) - period; i < len(closes); i++ {
		hl := highs[i].Sub(lows[i])
		hc := highs[i].Sub(closes[i-1]).Abs()
		lc := lows[i].Sub(closes[i-1]).Abs()
		tr := hl
		if hc.GreaterThan(tr) {
			tr = hc
		}
		if lc.GreaterThan(tr) {
			tr = lc
		}
		trs = append(trs, tr)
	}

	sum := decimal.Zero
	for _, tr := range trs {
		sum = sum.Add(tr)
	}
	return sum.Div(decimal.NewFromInt(int64(period)))
}

// --- MACD ---

// calcMACD は EMA(12) - EMA(26) = MACD、EMA(9, MACD) = Signal、MACD - Signal = Hist を返す。
// 最低 26+9-1=34 本のデータが必要。
func calcMACD(closes []decimal.Decimal) MACDResult {
	ema12 := calcEMA(closes, 12)
	ema26 := calcEMA(closes, 26)
	if ema12.IsZero() || ema26.IsZero() {
		return MACDResult{}
	}

	macdVal := ema12.Sub(ema26)

	// Signal: MACD 値の時系列が必要なため、各時点の MACD を再計算
	n := len(closes)
	macdSeries := make([]decimal.Decimal, 0, n-25)
	for i := 26; i <= n; i++ {
		e12 := calcEMA(closes[:i], 12)
		e26 := calcEMA(closes[:i], 26)
		macdSeries = append(macdSeries, e12.Sub(e26))
	}

	signal := calcEMA(macdSeries, 9)
	hist := macdVal.Sub(signal)

	return MACDResult{MACD: macdVal, Signal: signal, Hist: hist}
}
