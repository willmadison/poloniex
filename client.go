package poloniex

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"time"

	"github.com/jcelliott/turnpike"
	"github.com/pkg/errors"
)

// Client represents a Poloniex client.
type Client struct {
	turnpike *turnpike.Client
}

// Symbol represents single Poloniex ticker symbol entry.
type Symbol struct {
	FromCurrency, ToCurrency                                      string
	LastRate, LowestAsk, HighestBid, PercentageChange, BaseVolume float64
	Frozen                                                        bool
	DailyHigh                                                     float64
}

func (s Symbol) String() string {
	return fmt.Sprintf(`Symbol{FromCurrency: %s, ToCurrency: %s, LastRate: %1.9f, LowestAsk: %1.9f,
		HighestBid: %1.9f, PercentageChange: %5.5f,	BaseVolume: %9.7f, Frozen: %t, DailyHigh: %9.7f}`,
		s.FromCurrency, s.ToCurrency, s.LastRate, s.LowestAsk,
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

func toSymbol(tickerEntry []interface{}) (*Symbol, error) {
	s := &Symbol{}

	if currencyPair, ok := tickerEntry[currencyPairIndex].(string); ok {
		parts := strings.Split(currencyPair, "_")
		s.FromCurrency = parts[0]
		s.ToCurrency = parts[1]
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
func (c *Client) Ticker(ctx context.Context) <-chan *Symbol {
	symbols := make(chan *Symbol)

	go func() {
		err := c.turnpike.Subscribe("ticker", nil, turnpike.EventHandler(func(args []interface{}, kwargs map[string]interface{}) {
			symbol, err := toSymbol(args)

			if err != nil {
				fmt.Println("encountered an error converting an event to a Symbol:", err)
			}

			symbols <- symbol
		}))
		defer c.turnpike.Unsubscribe("ticker")
		defer close(symbols)

		if err != nil {
			fmt.Println("encountered an error subscribing to the 'ticker' topic:", errors.Wrap(err, "error subscribing to 'ticker' topic"))
		} else {
			for {
				select {
				case <-ctx.Done():
					goto Exit
				default:
					c.turnpike.Receive()
				}
			}
		}
	Exit:
	}()

	return symbols
}

func (c *Client) close() error {
	return c.turnpike.Close()
}

// New returns a new Poloniex client.
func New() (*Client, error) {
	client, err := turnpike.NewWebsocketClient(turnpike.JSON, "wss://api.poloniex.com", nil, nil)
	client.ReceiveTimeout = 5 * time.Second
	_, err = client.JoinRealm("realm1", nil)

	return &Client{turnpike: client}, err
}
