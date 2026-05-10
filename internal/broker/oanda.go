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

// NewOandaBroker は OandaBroker を生成する。practice=true のとき Practice 環境に接続する。
// TradingBroker と MarketBroker を両方実装した単一インスタンスを返す（historical モード等で使用）。
func NewOandaBroker(token, accountID string, practice bool) Broker {
	return newOandaBroker(token, accountID, practice)
}

// NewOandaTradingBroker は取引専用の TradingBroker を返す。
// practice=true のとき Practice 環境に接続する。
func NewOandaTradingBroker(token, accountID string, practice bool) TradingBroker {
	return newOandaBroker(token, accountID, practice)
}

// NewOandaMarketBroker はマーケットデータ専用の MarketBroker を返す。
// practice=true のとき Practice 環境に接続する。
// Labs API（calendar/COT/position ratios）はlive環境でのみ有効なため、
// 通常は practice=false で呼び出す。
func NewOandaMarketBroker(token string, practice bool) MarketBroker {
	return newOandaBroker(token, "", practice)
}

func newOandaBroker(token, accountID string, practice bool) *oandaBroker {
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

// --- FetchAccount ---

func (b *oandaBroker) FetchAccount(ctx context.Context) (pkgorder.AccountInfo, error) {
	path := "/v3/accounts/" + b.accountID + "/summary"
	resp, err := b.get(ctx, path, nil)
	if err != nil {
		return pkgorder.AccountInfo{}, fmt.Errorf("oanda: fetch account: %w", err)
	}
	data, err := readBody(resp)
	if err != nil {
		return pkgorder.AccountInfo{}, err
	}
	if err := checkStatus(resp, data, http.StatusOK); err != nil {
		return pkgorder.AccountInfo{}, err
	}

	var result struct {
		Account struct {
			Balance            string `json:"balance"`
			UnrealizedPL       string `json:"unrealizedPL"`
			MarginUsed         string `json:"marginUsed"`
			MarginAvailable    string `json:"marginAvailable"`
		} `json:"account"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return pkgorder.AccountInfo{}, fmt.Errorf("oanda: parse account: %w", err)
	}
	a := result.Account
	balance, _ := decimal.NewFromString(a.Balance)
	unrealized, _ := decimal.NewFromString(a.UnrealizedPL)
	marginUsed, _ := decimal.NewFromString(a.MarginUsed)
	marginAvail, _ := decimal.NewFromString(a.MarginAvailable)
	return pkgorder.AccountInfo{
		Balance:       balance,
		UnrealizedPnL: unrealized,
		MarginUsed:    marginUsed,
		MarginAvail:   marginAvail,
	}, nil
}

// --- FetchRate ---

// FetchRate は /v3/instruments/{instrument}/candles?price=BA&count=1 で最新Bid/Askを取得する。
// accountID 不要のため MarketBroker として単独で使える。
func (b *oandaBroker) FetchRate(ctx context.Context, pair currency.Pair) (currency.Rate, error) {
	instrument := toInstrument(pair)
	path := "/v3/instruments/" + instrument + "/candles"
	resp, err := b.get(ctx, path, map[string]string{
		"price":       "BA",
		"granularity": "S5",
		"count":       "1",
	})
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
		Candles []struct {
			Time string `json:"time"`
			Bid  struct {
				C string `json:"c"`
			} `json:"bid"`
			Ask struct {
				C string `json:"c"`
			} `json:"ask"`
		} `json:"candles"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return currency.Rate{}, fmt.Errorf("oanda: parse rate: %w", err)
	}
	if len(result.Candles) == 0 {
		return currency.Rate{}, fmt.Errorf("oanda: no candle for %s", pair)
	}
	c := result.Candles[0]
	bid, _ := decimal.NewFromString(c.Bid.C)
	ask, _ := decimal.NewFromString(c.Ask.C)
	ts, _ := time.Parse(time.RFC3339Nano, c.Time)
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
		Type         string        `json:"type"`
		Instrument   string        `json:"instrument"`
		Units        string        `json:"units"`
		Price        string        `json:"price,omitempty"`
		StopLossOn   stopLossOrder `json:"stopLossOnFill,omitempty"`
		TakeProfitOn *limitOrder   `json:"takeProfitOnFill,omitempty"`
	}

	body := orderBody{
		Instrument: instrument,
		Units:      units.String(),
		StopLossOn: stopLossOrder{Price: o.StopLoss.String()},
	}
	switch o.OrderType {
	case pkgorder.OrderTypeLimit:
		body.Type = "LIMIT"
		body.Price = o.LimitPrice.String()
	default:
		body.Type = "MARKET"
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
		case "LIMIT":
			ot = pkgorder.OrderTypeLimit
		case "STOP":
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
// sinceID != "" のとき /transactions/sinceid を使い全件取得（ページング不要）。
// sinceID == "" のとき直近100件を返す（初回起動時は過去データ不要）。
func (b *oandaBroker) FetchFillEvents(ctx context.Context, sinceID string) ([]pkgorder.FillEvent, error) {
	var (
		path   string
		params map[string]string
	)
	if sinceID != "" {
		// sinceid エンドポイントは指定ID以降の全トランザクションを返す（ページング不要）
		path = "/v3/accounts/" + b.accountID + "/transactions/sinceid"
		params = map[string]string{"id": sinceID}
	} else {
		path = "/v3/accounts/" + b.accountID + "/transactions"
		params = map[string]string{"type": "ORDER_FILL", "count": "100"}
	}
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

// --- FetchCalendar ---

const forexFactoryCalendarURL = "https://nfs.faireconomy.media/ff_calendar_thisweek.json"

// FetchCalendar は ForexFactory から今週の経済指標カレンダーを取得する。
// currencies が空でない場合は指定通貨のみ返す。
func (b *oandaBroker) FetchCalendar(ctx context.Context, currencies []string) ([]pkgorder.CalendarEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, forexFactoryCalendarURL, nil)
	if err != nil {
		return nil, fmt.Errorf("oanda: fetch calendar: %w", err)
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oanda: fetch calendar: %w", err)
	}
	data, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, data, http.StatusOK); err != nil {
		return nil, err
	}

	var raw []struct {
		Title    string `json:"title"`
		Country  string `json:"country"`
		Date     string `json:"date"`
		Impact   string `json:"impact"`
		Forecast string `json:"forecast"`
		Previous string `json:"previous"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("oanda: parse calendar: %w", err)
	}

	filter := make(map[string]bool, len(currencies))
	for _, c := range currencies {
		filter[c] = true
	}

	events := make([]pkgorder.CalendarEvent, 0, len(raw))
	for _, r := range raw {
		if len(filter) > 0 && !filter[r.Country] {
			continue
		}
		ts, _ := time.Parse(time.RFC3339, r.Date)
		events = append(events, pkgorder.CalendarEvent{
			Title:    r.Title,
			Country:  r.Country,
			Date:     ts.UTC(),
			Impact:   r.Impact,
			Forecast: r.Forecast,
			Previous: r.Previous,
		})
	}
	return events, nil
}
