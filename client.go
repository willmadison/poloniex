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
	turnpike   *turnpike.Client
	httpClient *http.Client
}

// Symbol represents a single Poloniex ticker symbol entry.
type Symbol struct {
	BaseCurrency, CounterCurrency   string
	LastRate, LowestAsk, HighestBid float64
	PercentageChange, BaseVolume    float64
	DailyHigh                       float64
	Frozen                          bool
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

func (s Symbol) String() string {
	return fmt.Sprintf(`Symbol{BaseCurrency: %s, CounterCurrency: %s, LastRate: %1.8f, LowestAsk: %1.8f,
		HighestBid: %1.8f, PercentageChange: %5.5f,	BaseVolume: %9.7f, Frozen: %t, DailyHigh: %1.8f}`,
		s.BaseCurrency, s.CounterCurrency, s.LastRate, s.LowestAsk,
		s.HighestBid, s.PercentageChange, s.BaseVolume, s.Frozen, s.DailyHigh)
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
func (c *Client) Balances(ctx context.Context, apiKey, apiSecret string) (map[string]Balance, error) {
	response, err := post(c.httpClient, "returnCompleteBalances", apiKey, apiSecret, url.Values{})
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
func (c *Client) TradeHistory(ctx context.Context, apiKey, apiSecret, baseCurrency, counterCurrency string, from, to time.Time) (map[string]TradeHistory, error) {
	params := url.Values{}

	var pair string

	if strings.TrimSpace(counterCurrency) == "" || strings.TrimSpace(baseCurrency) == "" {
		pair = "all"
	} else {
		pair = fmt.Sprintf("%s_%s", strings.ToUpper(counterCurrency), strings.ToUpper(baseCurrency))
	}

	params.Set("currencyPair", pair)
	params.Set("start", strconv.FormatInt(from.UTC().Unix(), 10))
	params.Set("end", strconv.FormatInt(to.UTC().Unix(), 10))

	response, err := post(c.httpClient, "returnTradeHistory", apiKey, apiSecret, params)
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
			BaseCurrency:    baseCurrency,
			CounterCurrency: counterCurrency,
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

type client interface {
	Do(req *http.Request) (*http.Response, error)
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
func New() (*Client, error) {
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
	}, err
}
