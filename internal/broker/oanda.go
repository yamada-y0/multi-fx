package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/pkg/currency"
	"github.com/yamada/fxd/pkg/market"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

const (
	oandaLiveHost     = "https://api-fxtrade.oanda.com"
	oandaPracticeHost = "https://api-fxpractice.oanda.com"
)

// oandaBroker は OANDA REST API v20 を使った Broker 実装
type oandaBroker struct {
	accountID  string
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewOandaBroker は OandaBroker を生成する。
// practice=true のとき Practice 環境に接続する。
func NewOandaBroker(token, accountID string, practice bool) Broker {
	base := oandaLiveHost
	if practice {
		base = oandaPracticeHost
	}
	return &oandaBroker{
		accountID:  accountID,
		token:      token,
		baseURL:    base,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (b *oandaBroker) Name() string { return "oanda" }

// --- HTTP ヘルパー ---

func (b *oandaBroker) get(ctx context.Context, path string, params map[string]string) (*http.Response, error) {
	url := b.baseURL + path
	if len(params) > 0 {
		parts := make([]string, 0, len(params))
		for k, v := range params {
			parts = append(parts, k+"="+v)
		}
		url += "?" + strings.Join(parts, "&")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Content-Type", "application/json")
	return b.httpClient.Do(req)
}

func (b *oandaBroker) post(ctx context.Context, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Content-Type", "application/json")
	return b.httpClient.Do(req)
}

func (b *oandaBroker) delete(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, b.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Content-Type", "application/json")
	return b.httpClient.Do(req)
}

func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func checkStatus(resp *http.Response, data []byte, expected int) error {
	if resp.StatusCode != expected {
		return fmt.Errorf("oanda: unexpected status %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

// --- ペア変換 ---

// toPair は OANDA の instrument 文字列（例: "USD_JPY"）を currency.Pair に変換する
func toPair(instrument string) currency.Pair {
	return currency.Pair(strings.ReplaceAll(instrument, "_", ""))
}

// toInstrument は currency.Pair（例: "USDJPY"）を OANDA の instrument 文字列に変換する
func toInstrument(pair currency.Pair) string {
	s := string(pair)
	if len(s) == 6 {
		return s[:3] + "_" + s[3:]
	}
	return s
}

// --- FetchRate ---

func (b *oandaBroker) FetchRate(ctx context.Context, pair currency.Pair) (currency.Rate, error) {
	instrument := toInstrument(pair)
	path := "/v3/accounts/" + b.accountID + "/pricing"
	resp, err := b.get(ctx, path, map[string]string{"instruments": instrument})
	if err != nil {
		return currency.Rate{}, fmt.Errorf("oanda: fetch rate: %w", err)
	}
	data, err := readBody(resp)
	if err != nil {
		return currency.Rate{}, err
	}
	if err := checkStatus(resp, data, http.StatusOK); err != nil {
		return currency.Rate{}, err
	}

	var result struct {
		Prices []struct {
			Instrument string `json:"instrument"`
			Bids       []struct {
				Price string `json:"price"`
			} `json:"bids"`
			Asks []struct {
				Price string `json:"price"`
			} `json:"asks"`
			Time string `json:"time"`
		} `json:"prices"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return currency.Rate{}, fmt.Errorf("oanda: parse pricing: %w", err)
	}
	if len(result.Prices) == 0 {
		return currency.Rate{}, fmt.Errorf("oanda: no price for %s", pair)
	}
	p := result.Prices[0]
	if len(p.Bids) == 0 || len(p.Asks) == 0 {
		return currency.Rate{}, fmt.Errorf("oanda: empty bid/ask for %s", pair)
	}
	bid, err := decimal.NewFromString(p.Bids[0].Price)
	if err != nil {
		return currency.Rate{}, fmt.Errorf("oanda: parse bid: %w", err)
	}
	ask, err := decimal.NewFromString(p.Asks[0].Price)
	if err != nil {
		return currency.Rate{}, fmt.Errorf("oanda: parse ask: %w", err)
	}
	ts, _ := time.Parse(time.RFC3339Nano, p.Time)
	return currency.Rate{Pair: pair, Bid: bid, Ask: ask, Timestamp: ts}, nil
}

// --- FetchCandles ---

func (b *oandaBroker) FetchCandles(ctx context.Context, pair currency.Pair, granularity string, count int) ([]market.Candle, error) {
	instrument := toInstrument(pair)
	path := "/v3/instruments/" + instrument + "/candles"
	resp, err := b.get(ctx, path, map[string]string{
		"granularity": granularity,
		"count":       strconv.Itoa(count),
		"price":       "M", // mid
	})
	if err != nil {
		return nil, fmt.Errorf("oanda: fetch candles: %w", err)
	}
	data, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, data, http.StatusOK); err != nil {
		return nil, err
	}

	var result struct {
		Candles []struct {
			Time     string `json:"time"`
			Complete bool   `json:"complete"`
			Mid      struct {
				O string `json:"o"`
				H string `json:"h"`
				L string `json:"l"`
				C string `json:"c"`
			} `json:"mid"`
			Volume int `json:"volume"`
		} `json:"candles"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("oanda: parse candles: %w", err)
	}

	candles := make([]market.Candle, 0, len(result.Candles))
	for i := len(result.Candles) - 1; i >= 0; i-- { // 新しい順に並び替え
		c := result.Candles[i]
		ts, _ := time.Parse(time.RFC3339Nano, c.Time)
		open, _ := decimal.NewFromString(c.Mid.O)
		high, _ := decimal.NewFromString(c.Mid.H)
		low, _ := decimal.NewFromString(c.Mid.L)
		cls, _ := decimal.NewFromString(c.Mid.C)
		candles = append(candles, market.Candle{
			Timestamp: ts,
			Pair:      pair,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     cls,
			Volume:    decimal.NewFromInt(int64(c.Volume)),
		})
	}
	return candles, nil
}

// --- SubmitOrder ---

func (b *oandaBroker) SubmitOrder(ctx context.Context, o pkgorder.Order) (OrderID, error) {
	if o.Intent == pkgorder.OrderIntentClose {
		return b.closePosition(ctx, o)
	}
	return b.openOrder(ctx, o)
}

func (b *oandaBroker) openOrder(ctx context.Context, o pkgorder.Order) (OrderID, error) {
	instrument := toInstrument(o.Pair)

	// OANDA: Long は正のユニット数、Short は負
	units := o.Lots.Mul(decimal.NewFromInt(100000)) // 1lot = 100,000通貨
	if o.Side == pkgorder.Short {
		units = units.Neg()
	}

	type stopLossOrder struct {
		Price string `json:"price"`
	}
	type limitOrder struct {
		Price string `json:"price"`
	}
	type orderBody struct {
		Type        string        `json:"type"`
		Instrument  string        `json:"instrument"`
		Units       string        `json:"units"`
		StopLossOn  stopLossOrder `json:"stopLossOnFill,omitempty"`
		TakeProfitOn *limitOrder  `json:"takeProfitOnFill,omitempty"`
	}

	body := orderBody{
		Instrument: instrument,
		Units:      units.String(),
		StopLossOn: stopLossOrder{Price: o.StopLoss.String()},
	}
	switch o.OrderType {
	case pkgorder.OrderTypeLimit:
		body.Type = "LIMIT_ORDER"
		body.TakeProfitOn = &limitOrder{Price: o.LimitPrice.String()}
	default:
		body.Type = "MARKET_ORDER"
	}

	payload, err := json.Marshal(map[string]any{"order": body})
	if err != nil {
		return "", err
	}

	resp, err := b.post(ctx, "/v3/accounts/"+b.accountID+"/orders", strings.NewReader(string(payload)))
	if err != nil {
		return "", fmt.Errorf("oanda: submit order: %w", err)
	}
	data, err := readBody(resp)
	if err != nil {
		return "", err
	}
	if err := checkStatus(resp, data, http.StatusCreated); err != nil {
		return "", err
	}

	var result struct {
		OrderCreateTransaction struct {
			ID string `json:"id"`
		} `json:"orderCreateTransaction"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("oanda: parse order response: %w", err)
	}
	return OrderID(result.OrderCreateTransaction.ID), nil
}

func (b *oandaBroker) closePosition(ctx context.Context, o pkgorder.Order) (OrderID, error) {
	path := "/v3/accounts/" + b.accountID + "/trades/" + o.ClosePositionID + "/close"
	payload := `{"units": "ALL"}`
	resp, err := b.post(ctx, path, strings.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("oanda: close position: %w", err)
	}
	data, err := readBody(resp)
	if err != nil {
		return "", err
	}
	if err := checkStatus(resp, data, http.StatusOK); err != nil {
		return "", err
	}

	var result struct {
		OrderFillTransaction struct {
			ID string `json:"id"`
		} `json:"orderFillTransaction"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("oanda: parse close response: %w", err)
	}
	return OrderID(result.OrderFillTransaction.ID), nil
}

// --- CancelOrder ---

func (b *oandaBroker) CancelOrder(ctx context.Context, id OrderID) error {
	path := "/v3/accounts/" + b.accountID + "/orders/" + string(id) + "/cancel"
	resp, err := b.post(ctx, path, nil)
	if err != nil {
		return fmt.Errorf("oanda: cancel order: %w", err)
	}
	data, err := readBody(resp)
	if err != nil {
		return err
	}
	return checkStatus(resp, data, http.StatusOK)
}

// --- FetchPositions ---

func (b *oandaBroker) FetchPositions(ctx context.Context) ([]pkgorder.Position, error) {
	path := "/v3/accounts/" + b.accountID + "/openTrades"
	resp, err := b.get(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("oanda: fetch positions: %w", err)
	}
	data, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, data, http.StatusOK); err != nil {
		return nil, err
	}

	var result struct {
		Trades []struct {
			ID           string `json:"id"`
			Instrument   string `json:"instrument"`
			CurrentUnits string `json:"currentUnits"`
			Price        string `json:"price"`
			OpenTime     string `json:"openTime"`
		} `json:"trades"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("oanda: parse trades: %w", err)
	}

	positions := make([]pkgorder.Position, 0, len(result.Trades))
	for _, t := range result.Trades {
		units, _ := decimal.NewFromString(t.CurrentUnits)
		openPrice, _ := decimal.NewFromString(t.Price)
		openedAt, _ := time.Parse(time.RFC3339Nano, t.OpenTime)

		side := pkgorder.Long
		if units.IsNegative() {
			side = pkgorder.Short
			units = units.Abs()
		}
		lots := units.Div(decimal.NewFromInt(100000))

		positions = append(positions, pkgorder.Position{
			ID:        t.ID,
			Pair:      toPair(t.Instrument),
			Side:      side,
			Lots:      lots,
			OpenPrice: openPrice,
			OpenedAt:  openedAt,
		})
	}
	return positions, nil
}

// --- FetchOrders ---

func (b *oandaBroker) FetchOrders(ctx context.Context) ([]pkgorder.PendingOrder, error) {
	path := "/v3/accounts/" + b.accountID + "/pendingOrders"
	resp, err := b.get(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("oanda: fetch orders: %w", err)
	}
	data, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, data, http.StatusOK); err != nil {
		return nil, err
	}

	var result struct {
		Orders []struct {
			ID         string `json:"id"`
			Instrument string `json:"instrument"`
			Units      string `json:"units"`
			Type       string `json:"type"`
			Price      string `json:"price"`
		} `json:"orders"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("oanda: parse pending orders: %w", err)
	}

	orders := make([]pkgorder.PendingOrder, 0, len(result.Orders))
	for _, o := range result.Orders {
		units, _ := decimal.NewFromString(o.Units)
		side := pkgorder.Long
		if units.IsNegative() {
			side = pkgorder.Short
			units = units.Abs()
		}
		lots := units.Div(decimal.NewFromInt(100000))

		var ot pkgorder.OrderType
		switch o.Type {
		case "LIMIT_ORDER":
			ot = pkgorder.OrderTypeLimit
		case "STOP_ORDER":
			ot = pkgorder.OrderTypeStop
		default:
			ot = pkgorder.OrderTypeMarket
		}

		limitPrice, _ := decimal.NewFromString(o.Price)
		orders = append(orders, pkgorder.PendingOrder{
			ID: o.ID,
			Order: pkgorder.Order{
				Pair:       toPair(o.Instrument),
				Side:       side,
				Lots:       lots,
				OrderType:  ot,
				LimitPrice: limitPrice,
			},
		})
	}
	return orders, nil
}

// --- FetchFillEvents ---

// FetchFillEvents は OANDA のトランザクション履歴から約定イベントを取得する。
// sinceID="" のとき直近100件を返す。
func (b *oandaBroker) FetchFillEvents(ctx context.Context, sinceID string) ([]pkgorder.FillEvent, error) {
	params := map[string]string{"type": "ORDER_FILL"}
	if sinceID != "" {
		params["sinceTransactionID"] = sinceID
	} else {
		params["count"] = "100"
	}
	path := "/v3/accounts/" + b.accountID + "/transactions"
	resp, err := b.get(ctx, path, params)
	if err != nil {
		return nil, fmt.Errorf("oanda: fetch transactions: %w", err)
	}
	data, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, data, http.StatusOK); err != nil {
		return nil, err
	}

	var result struct {
		Transactions []struct {
			ID         string `json:"id"`
			OrderID    string `json:"orderID"`
			TradeID    string `json:"tradeID"`  // Open時のPositionID
			TradeClose struct {
				TradeID string `json:"tradeID"` // Close時のPositionID
			} `json:"tradesClosed"`
			Instrument string `json:"instrument"`
			Units      string `json:"units"`
			Price      string `json:"price"`
			Time       string `json:"time"`
			Type       string `json:"type"`
			Reason     string `json:"reason"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("oanda: parse transactions: %w", err)
	}

	events := make([]pkgorder.FillEvent, 0, len(result.Transactions))
	for _, tx := range result.Transactions {
		if tx.Type != "ORDER_FILL" {
			continue
		}
		units, _ := decimal.NewFromString(tx.Units)
		side := pkgorder.Long
		if units.IsNegative() {
			side = pkgorder.Short
			units = units.Abs()
		}
		lots := units.Div(decimal.NewFromInt(100000))
		price, _ := decimal.NewFromString(tx.Price)
		ts, _ := time.Parse(time.RFC3339Nano, tx.Time)

		// Close かどうかは tradesClosed が非空かで判断
		intent := pkgorder.OrderIntentOpen
		positionID := tx.TradeID
		if tx.TradeClose.TradeID != "" {
			intent = pkgorder.OrderIntentClose
			positionID = tx.TradeClose.TradeID
		}

		events = append(events, pkgorder.FillEvent{
			ID:          tx.ID,
			OrderID:     tx.OrderID,
			PositionID:  positionID,
			Intent:      intent,
			Pair:        toPair(tx.Instrument),
			Side:        side,
			Lots:        lots,
			FilledPrice: price,
			FilledAt:    ts,
		})
	}
	return events, nil
}
