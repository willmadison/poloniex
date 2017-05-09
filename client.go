package poloniex

import (
	"context"

	"github.com/jcelliott/turnpike"
)

type Client struct {
	turnpike *turnpike.Client
}

type Symbol struct {
	FromCurrency, ToCurrency                                      string
	LastRate, LowestAsk, HighestBid, PercentageChange, BaseVolume float32
	Frozen                                                        bool
	DailyHigh                                                     float32
}

func (c *Client) Ticker(ctx context.Context) <-chan Symbol {
	return make(chan Symbol)
}

func (c *Client) close() error {
	return c.turnpike.Close()
}

func New() (*Client, error) {
	router := turnpike.NewDefaultRouter()
	router.RegisterRealm(turnpike.URI("api.poloniex.com"), turnpike.Realm{})
	peer, err := router.GetLocalPeer(turnpike.URI("api.poloniex.com"), map[string]interface{}{})

	if err != nil {
		return nil, err
	}

	client := turnpike.NewClient(peer)

	return &Client{turnpike: client}, nil
}
