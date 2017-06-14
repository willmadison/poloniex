package poloniex

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"time"

	"github.com/jcelliott/turnpike"
	"github.com/pkg/errors"
)

const (
	// DateFormat is the default date format used on Poloniex.
	DateFormat = "2006-01-02 15:04:05"
)

// Client represents a Poloniex client.
type Client struct {
	turnpike          *turnpike.Client
	httpClient        *http.Client
	apiKey, apiSecret string
}

// Symbol represents a single Poloniex ticker symbol entry.
type Symbol struct {
	BaseCurrency, CounterCurrency   string
	LastRate, LowestAsk, HighestBid float64
	PercentageChange, BaseVolume    float64
	DailyHigh                       float64
	Frozen                          bool
}

func (s Symbol) String() string {
	return fmt.Sprintf(`Symbol{BaseCurrency: %s, CounterCurrency: %s, LastRate: %1.8f, LowestAsk: %1.8f,
		HighestBid: %1.8f, PercentageChange: %5.5f,	BaseVolume: %9.7f, Frozen: %t, DailyHigh: %1.8f}`,
		s.BaseCurrency, s.CounterCurrency, s.LastRate, s.LowestAsk,
		s.HighestBid, s.PercentageChange, s.BaseVolume, s.Frozen, s.DailyHigh)
}

// Balance represents the complete balance (available, on orders, and BTC value of a given currency)
type Balance struct {
	Available, OnOrders, BTCValue float64
}

// TradeHistory represents the macro level analysis of the trade history of a given currency pair
type TradeHistory struct {
	BaseCurrency, CounterCurrency                     string
	AverageSellPrice, AverageBuyPrice, BreakEvenPrice float64
	Trades                                            []Trade
}

func (t TradeHistory) String() string {
	return fmt.Sprintf(`TradeHistory{BaseCurrency: %s, CounterCurrency: %s, AverageSellPrice: %1.8f, AverageBuyPrice: %1.8f,
		BreakEvenPrice: %1.8f, Trades: %v}`,
		t.BaseCurrency, t.CounterCurrency, t.AverageSellPrice, t.AverageBuyPrice, t.BreakEvenPrice, t.Trades)
}

// Criterion represents the trade history search criterion.
type Criterion struct {
	BaseCurrency, CounterCurrency string
	From, To                      time.Time
}

var nilTime time.Time

// WithTimeFrame returns a functional option which configures a Criterion time window according to the given to/from times.
func WithTimeFrame(from, to time.Time) func(*Criterion) {
	return func(c *Criterion) {
		c.From, c.To = from, to
	}
}

// WithCurrencyPair returns a functional option which configures a Criterion with the given currency pair.
func WithCurrencyPair(baseCurrency, counterCurrency string) func(*Criterion) {
	return func(c *Criterion) {
		c.BaseCurrency, c.CounterCurrency = strings.TrimSpace(strings.ToUpper(baseCurrency)), strings.TrimSpace(strings.ToUpper(counterCurrency))
	}
}

func (t *TradeHistory) analyze() {
	sells := 0
	buys := 0

	totalSellPrice := 0.0
	totalBuyPrice := 0.0

	for _, trade := range t.Trades {
		if trade.Category == "exchange" {
			switch trade.Type {
			case "buy":
				totalBuyPrice += trade.Rate
				buys++
			case "sell":
				totalSellPrice += trade.Rate
				sells++
			}
		}
	}

	if sells > 0 {
		t.AverageSellPrice = totalSellPrice / float64(sells)
	}

	if buys > 0 {
		t.AverageBuyPrice = totalBuyPrice / float64(buys)
	}
}

// Trade represents a discrete trade event (i.e. buy/sell) of a given currency pair
type Trade struct {
	GlobalTradeID               int64
	TradeID                     int64
	Date                        time.Time
	Rate, Amount, Total, Fee    float64
	OrderNumber, Type, Category string
}

func (t Trade) String() string {
	return fmt.Sprintf(`Trade{GlobalTradeID: %d, TradeID: %d, Date: %v, Rate: %1.8f, Amount: %1.8f,
		Total: %6.8f, Fee: %1.8f, OrderNumber: %s, Type: %s, Category: %s}`,
		t.GlobalTradeID, t.TradeID, t.Date, t.Rate, t.Amount, t.Total, t.Fee, t.OrderNumber, t.Type, t.Category)
}

// OrderOption represents additional options for order (i.e. buy/sell) placement.
type OrderOption struct {
	FillOrKill        bool
	ImmediateOrCancel bool
	PostOnly          bool
}

// WithFillOrKill returns a functional option which configures a OrderOption to fillOrKill an order immediately.
func WithFillOrKill() func(*OrderOption) {
	return func(o *OrderOption) {
		o.FillOrKill = true
	}
}

// WithImmediateOrCancel returns a functional option which configures a OrderOption to place an order such that unfilled bits are immediately cancelled.
func WithImmediateOrCancel() func(*OrderOption) {
	return func(o *OrderOption) {
		o.ImmediateOrCancel = true
	}
}

// WithPostOnly returns a functional option which configures a OrderOption to place an order such that it will only be posted if no part of it fill imeediately.
func WithPostOnly() func(*OrderOption) {
	return func(o *OrderOption) {
		o.PostOnly = true
	}
}

// Receipt is an order receipt.
type Receipt struct {
	OrderNumber int64
	Trades      []struct {
		Amount, Total float64
		Type, TradeID string
		Date          time.Time
	}
}

// FeeSchedule represents the Poloniex fee schedule for an individual trader.
type FeeSchedule struct {
	MakerFee, TakerFee, ThirtyDayVolume float64
}

const (
	currencyPairIndex int = iota
	lastRateIndex
	lowestAskIndex
	highestBidIndex
	percentageChangeIndex
	baseVolumeIndex
	_
	frozenIndex
	dailyHighIndex
)

const (
	host = "poloniex.com"
	path = "/tradingApi"
)

var poloniexURL = &url.URL{
	Scheme: "https",
	Host:   host,
	Path:   path,
}

func toSymbol(tickerEntry []interface{}) (*Symbol, error) {
	s := &Symbol{}

	if currencyPair, ok := tickerEntry[currencyPairIndex].(string); ok {
		parts := strings.Split(currencyPair, "_")
		s.BaseCurrency, s.CounterCurrency = parts[1], parts[0]
	}

	var err error

	if lastRate, ok := tickerEntry[lastRateIndex].(string); ok {
		s.LastRate, err = strconv.ParseFloat(lastRate, 64)
		if err != nil {
			return s, errors.Wrap(err, "encountered an error attempting to parse the last rate as a floating point number")
		}
	}

	if lowestAsk, ok := tickerEntry[lowestAskIndex].(string); ok {
		s.LowestAsk, err = strconv.ParseFloat(lowestAsk, 64)
		if err != nil {
			return s, errors.Wrap(err, "encountered an error attempting to parse the lowest ask as a floating point number")
		}
	}

	if highestBid, ok := tickerEntry[highestBidIndex].(string); ok {
		s.HighestBid, err = strconv.ParseFloat(highestBid, 64)
		if err != nil {
			return s, errors.Wrap(err, "encountered an error attempting to parse the highest bid as a floating point number")
		}
	}

	if percentageChange, ok := tickerEntry[percentageChangeIndex].(string); ok {
		s.PercentageChange, err = strconv.ParseFloat(percentageChange, 64)
		if err != nil {
			return s, errors.Wrap(err, "encountered an error attempting to parse the percentage change as a floating point number")
		}
	}

	if baseVolume, ok := tickerEntry[baseVolumeIndex].(string); ok {
		s.BaseVolume, err = strconv.ParseFloat(baseVolume, 64)
		if err != nil {
			return s, errors.Wrap(err, "encountered an error attempting to parse the base volume as a floating point number")
		}
	}

	if frozen, ok := tickerEntry[frozenIndex].(string); ok {
		s.Frozen, err = strconv.ParseBool(frozen)
		if err != nil {
			return s, errors.Wrap(err, "encountered an error attempting to parse the frozen value as a boolean")
		}
	}

	if dailyHigh, ok := tickerEntry[dailyHighIndex].(string); ok {
		s.DailyHigh, err = strconv.ParseFloat(dailyHigh, 64)
		if err != nil {
			return s, errors.Wrap(err, "encountered an error attempting to parse the daily high as a floating point number")
		}
	}

	return s, nil
}

// Ticker returns a read-only channel of Poloniex ticker symbol entries.
func (c *Client) Ticker(ctx context.Context) (<-chan *Symbol, error) {
	symbols := make(chan *Symbol)
	var once sync.Once

	err := c.turnpike.Subscribe("ticker", nil, turnpike.EventHandler(func(args []interface{}, kwargs map[string]interface{}) {
		symbol, err := toSymbol(args)
		if err != nil {
			fmt.Println("encountered an error converting an event to a Symbol:", err)
		}

		select {
		case <-ctx.Done():
			once.Do(func() {
				close(symbols)
				symbols = nil

				if err := c.turnpike.Unsubscribe("ticker"); err != nil {
					fmt.Println("encountered error during unsubscription:", err)
				}
			})
		case symbols <- symbol:
		}
	}))

	if err != nil {
		fmt.Println("encountered an error subscribing to the 'ticker' topic:", errors.Wrap(err, "error subscribing to 'ticker' topic"))
		return nil, errors.WithStack(err)
	}

	return symbols, nil
}

type apiError struct {
	Error string `json:"error"`
}

type balanceInquiry struct {
	Available string `json:"available"`
	OnOrders  string `json:"onOrders"`
	BTCValue  string `json:"btcValue"`
}

func (b balanceInquiry) asBalance() (Balance, error) {
	balance := Balance{}

	var err error

	balance.Available, err = strconv.ParseFloat(b.Available, 64)
	if err != nil {
		return balance, errors.Wrap(err, "encountered an error attempting to parse the available amount as a floating point number")
	}

	balance.OnOrders, err = strconv.ParseFloat(b.OnOrders, 64)
	if err != nil {
		return balance, errors.Wrap(err, "encountered an error attempting to parse the onOrders amount as a floating point number")
	}

	balance.BTCValue, err = strconv.ParseFloat(b.BTCValue, 64)
	if err != nil {
		return balance, errors.Wrap(err, "encountered an error attempting to parse the btcValue amount as a floating point number")
	}

	return balance, nil
}

// Balances returns the current balances of your Poloniex account.
func (c *Client) Balances(ctx context.Context) (map[string]Balance, error) {
	response, err := post(c.httpClient, "returnCompleteBalances", c.apiKey, c.apiSecret, url.Values{})
	if err != nil {
		return nil, err
	}

	var apiError apiError

	_ = json.Unmarshal(response, &apiError)
	if apiError.Error != "" {
		return nil, errors.New(apiError.Error)
	}

	inquiries := map[string]balanceInquiry{}
	err = json.Unmarshal(response, &inquiries)
	if err != nil {
		return nil, err
	}

	balances := map[string]Balance{}

	for symbol, inquiry := range inquiries {
		if balances[symbol], err = inquiry.asBalance(); err != nil {
			return balances, err
		}
	}

	return balances, nil
}

type tradeHistoryResponse struct {
	GlobalTradeID int64  `json:"globalTradeID"`
	TradeID       string `json:"tradeID"`
	Date          string `json:"date"`
	Rate          string `json:"rate"`
	Amount        string `json:"amount"`
	Total         string `json:"total"`
	Fee           string `json:"fee"`
	OrderNumber   string `json:"orderNumber"`
	Type          string `json:"type"`
	Category      string `json:"category"`
}

func (t tradeHistoryResponse) asTrade() (Trade, error) {
	trade := Trade{
		GlobalTradeID: t.GlobalTradeID,
		OrderNumber:   t.OrderNumber,
		Type:          t.Type,
		Category:      t.Category,
	}

	tradeID, err := strconv.Atoi(t.TradeID)
	if err != nil {
		return trade, errors.Wrap(err, "encountered an error converting the tradeID to an integer")
	}
	trade.TradeID = int64(tradeID)

	trade.Date, err = time.Parse(DateFormat, t.Date)
	if err != nil {
		return trade, errors.Wrap(err, "encountered an error attempting to parse the trade date as a time.Time")
	}

	trade.Rate, err = strconv.ParseFloat(t.Rate, 64)
	if err != nil {
		return trade, errors.Wrap(err, "encountered an error attempting to parse the trade rate as a floating point number")
	}

	trade.Amount, err = strconv.ParseFloat(t.Amount, 64)
	if err != nil {
		return trade, errors.Wrap(err, "encountered an error attempting to parse the trade amount as a floating point number")
	}

	trade.Total, err = strconv.ParseFloat(t.Total, 64)
	if err != nil {
		return trade, errors.Wrap(err, "encountered an error attempting to parse the trade total as a floating point number")
	}

	trade.Fee, err = strconv.ParseFloat(t.Fee, 64)
	if err != nil {
		return trade, errors.Wrap(err, "encountered an error attempting to parse the trade fee a floating point number")
	}

	return trade, nil
}

// TradeHistory returns the trade history of the given currency pair from your Poloniex account.
func (c *Client) TradeHistory(ctx context.Context, options ...func(*Criterion)) (map[string]TradeHistory, error) {
	criterion := &Criterion{}

	for _, option := range options {
		option(criterion)
	}

	params := url.Values{}

	pair := "all"

	if criterion.CounterCurrency != "" && criterion.BaseCurrency != "" {
		pair = fmt.Sprintf("%s_%s", criterion.CounterCurrency, criterion.BaseCurrency)
	}

	params.Set("currencyPair", pair)

	if criterion.From != nilTime {
		params.Set("start", strconv.FormatInt(criterion.From.UTC().Unix(), 10))
	}

	if criterion.To != nilTime {
		params.Set("end", strconv.FormatInt(criterion.To.UTC().Unix(), 10))
	}

	response, err := post(c.httpClient, "returnTradeHistory", c.apiKey, c.apiSecret, params)
	if err != nil {
		return nil, err
	}

	var apiError apiError

	_ = json.Unmarshal(response, &apiError)
	if apiError.Error != "" {
		return nil, errors.New(apiError.Error)
	}

	history := map[string]TradeHistory{}

	if pair != "all" {
		var trades []tradeHistoryResponse

		err = json.Unmarshal(response, &trades)
		if err != nil {
			return nil, errors.Wrap(err, "encountered an error unmarshalling a trade history response")
		}

		tradeHistory := &TradeHistory{
			BaseCurrency:    criterion.BaseCurrency,
			CounterCurrency: criterion.CounterCurrency,
		}

		for _, t := range trades {
			trade, err := t.asTrade()

			if err != nil {
				return nil, err
			}

			tradeHistory.Trades = append(tradeHistory.Trades, trade)
		}

		tradeHistory.analyze()
		history[pair] = *tradeHistory
	} else {
		var emptyResponse []interface{}
		err = json.Unmarshal(response, &emptyResponse)
		if err == nil {
			return history, nil
		}

		var tradesByCurrencyPair map[string][]tradeHistoryResponse

		err = json.Unmarshal(response, &tradesByCurrencyPair)
		if err != nil {
			return nil, errors.Wrap(err, "encountered an error unmarshalling a trade history response")
		}

		for currencyPair, trades := range tradesByCurrencyPair {
			tradeHistory := &TradeHistory{}

			parts := strings.Split(currencyPair, "_")
			tradeHistory.BaseCurrency, tradeHistory.CounterCurrency = parts[1], parts[0]

			for _, t := range trades {
				trade, err := t.asTrade()

				if err != nil {
					return nil, err
				}

				tradeHistory.Trades = append(tradeHistory.Trades, trade)
			}

			tradeHistory.analyze()
			history[currencyPair] = *tradeHistory
		}
	}

	return history, nil
}

// Buy issues a buy order to the Poloniex exchange for the given currency pair, rate, and amount.
func (c *Client) Buy(ctx context.Context, baseCurrency, counterCurrency string, amount, rate float64, options ...func(*OrderOption)) (Receipt, error) {
	return transact(ctx, c.httpClient, "buy", c.apiKey, c.apiSecret, baseCurrency, counterCurrency, amount, rate, options...)
}

// Sell issues a sell order to the Poloniex exchange for the given currency pair, rate, and amount.
func (c *Client) Sell(ctx context.Context, baseCurrency, counterCurrency string, amount, rate float64, options ...func(*OrderOption)) (Receipt, error) {
	return transact(ctx, c.httpClient, "sell", c.apiKey, c.apiSecret, baseCurrency, counterCurrency, amount, rate, options...)
}

type client interface {
	Do(req *http.Request) (*http.Response, error)
}

type buySellResponse struct {
	OrderNumber     string `json:"orderNumber"`
	ResultingTrades []struct {
		Amount  string `json:"amount"`
		Date    string `json:"date"`
		Rate    string `json:"rate"`
		Total   string `json:"total"`
		TradeID string `json:"tradeID"`
		Type    string `json:"type"`
	} `json:"resultingTrades"`
}

func (b buySellResponse) asReceipt() (Receipt, error) {
	r := Receipt{}
	var err error

	r.OrderNumber, err = strconv.ParseInt(b.OrderNumber, 10, 64)

	for _, trade := range b.ResultingTrades {
		amt, err := strconv.ParseFloat(trade.Amount, 64)
		if err != nil {
			return r, err
		}

		total, err := strconv.ParseFloat(trade.Total, 64)
		if err != nil {
			return r, err
		}

		date, err := time.Parse(DateFormat, trade.Date)
		if err != nil {
			return r, err
		}

		r.Trades = append(r.Trades, struct {
			Amount, Total float64
			Type, TradeID string
			Date          time.Time
		}{
			Amount:  amt,
			Total:   total,
			Type:    trade.Type,
			TradeID: trade.TradeID,
			Date:    date,
		})
	}

	return r, err
}

func transact(ctx context.Context, c client, buyOrSell, apiKey, apiSecret, baseCurrency, counterCurrency string, amount, rate float64, options ...func(*OrderOption)) (Receipt, error) {
	o := &OrderOption{}

	for _, option := range options {
		option(o)
	}

	params := url.Values{}

	var pair string

	if baseCurrency != "" && counterCurrency != "" {
		pair = strings.ToUpper(fmt.Sprintf("%s_%s", counterCurrency, baseCurrency))
	}

	params.Set("currencyPair", pair)
	params.Set("rate", strconv.FormatFloat(rate, 'f', -1, 64))
	params.Set("amount", strconv.FormatFloat(amount, 'f', -1, 64))

	switch {
	case o.FillOrKill:
		params.Set("fillOrKill", "1")
	case o.ImmediateOrCancel:
		params.Set("immediateOrCancel", "1")
	case o.PostOnly:
		params.Set("postOnly", "1")
	}

	response, err := post(c, buyOrSell, apiKey, apiSecret, params)
	if err != nil {
		return Receipt{}, err
	}

	var apiError apiError

	_ = json.Unmarshal(response, &apiError)
	if apiError.Error != "" {
		return Receipt{}, errors.New(apiError.Error)
	}

	var b buySellResponse

	err = json.Unmarshal(response, &b)
	if err != nil {
		return Receipt{}, errors.Wrap(err, "encountered an error attempting to parse the buy/sell response")
	}

	return b.asReceipt()
}

type feeInfo struct {
	MakerFee        string `json:"makerFee"`
	TakerFee        string `json:"takerFee"`
	ThirtyDayVolume string `json:"thirtyDayVolume"`
}

// FeeSchedule returns the current fee schedule based on the caller's 30 BTC volume.
func (c *Client) FeeSchedule(ctx context.Context) (FeeSchedule, error) {
	response, err := post(c.httpClient, "returnFeeInfo", c.apiKey, c.apiSecret, url.Values{})
	if err != nil {
		return FeeSchedule{}, err
	}

	var apiError apiError

	_ = json.Unmarshal(response, &apiError)
	if apiError.Error != "" {
		return FeeSchedule{}, errors.New(apiError.Error)
	}

	var f feeInfo

	err = json.Unmarshal(response, &f)
	if err != nil {
		return FeeSchedule{}, errors.Wrap(err, "encountered an error attempting to parse the fee info")
	}

	makerFee, err := strconv.ParseFloat(f.MakerFee, 64)
	if err != nil {
		return FeeSchedule{}, errors.Wrap(err, "encountered an error attempting to parse the makerFee")
	}

	takerFee, err := strconv.ParseFloat(f.TakerFee, 64)
	if err != nil {
		return FeeSchedule{}, errors.Wrap(err, "encountered an error attempting to parse the takerFee")
	}

	thirtyDayVol, err := strconv.ParseFloat(f.ThirtyDayVolume, 64)
	if err != nil {
		return FeeSchedule{}, errors.Wrap(err, "encountered an error attempting to parse the thirtyDayVol")
	}

	return FeeSchedule{
		MakerFee:        makerFee,
		TakerFee:        takerFee,
		ThirtyDayVolume: thirtyDayVol,
	}, nil
}

// CancelOrder attempts to cancel the given order number.
func (c *Client) CancelOrder(ctx context.Context, orderNumber int64) error {
	params := url.Values{}
	params.Set("orderNumber", strconv.FormatInt(orderNumber, 10))
	response, err := post(c.httpClient, "cancelOrder", c.apiKey, c.apiSecret, params)
	if err != nil {
		return err
	}

	var apiError apiError

	_ = json.Unmarshal(response, &apiError)
	if apiError.Error != "" {
		return errors.New(apiError.Error)
	}

	var result struct {
		Success int `json:"success"`
	}

	err = json.Unmarshal(response, &result)
	if err != nil {
		return errors.Wrap(err, "encountered an error attempting to parse the order cancellation response")
	}

	if result.Success != 1 {
		return errors.New("unable to cancel order #" + strconv.FormatInt(orderNumber, 10))
	}

	return nil
}

func post(c client, command, apiKey, apiSecret string, params url.Values) (json.RawMessage, error) {
	params.Set("command", command)
	params.Set("nonce", strconv.FormatInt(time.Now().UnixNano(), 10))

	body := params.Encode()
	request, err := http.NewRequest(http.MethodPost, poloniexURL.String(), strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	mac := hmac.New(sha512.New, []byte(apiSecret))
	mac.Write([]byte(body))
	signedBody := hex.EncodeToString(mac.Sum(nil))

	request.Header.Set("Key", apiKey)
	request.Header.Set("Sign", signedBody)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := c.Do(request)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	responseBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	return json.RawMessage(responseBody), nil
}

// Close closes a Poloniex client, freeing any resources.
func (c *Client) Close() error {
	return c.turnpike.Close()
}

// New returns a new Poloniex client.
func New(apiKey, apiSecret string) (*Client, error) {
	client, err := turnpike.NewWebsocketClient(turnpike.JSON, "wss://api.poloniex.com", nil, nil)
	if err != nil {
		return nil, errors.Wrap(err, "encountered an error initializing a new turnpike client")
	}

	client.ReceiveTimeout = 30 * time.Second
	_, err = client.JoinRealm("realm1", nil)

	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 35 * time.Second,
			}).Dial,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConnsPerHost: 500,
		},
	}

	return &Client{
		turnpike:   client,
		httpClient: httpClient,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
	}, err
}
