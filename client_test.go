package poloniex_test

import (
	"context"
	"testing"
	"time"

	"os"

	"github.com/willmadison/poloniex"
)

func TestTicker(t *testing.T) {
	p, err := poloniex.New()

	if err != nil {
		t.Fatal("encountered an unexpected error:", err.Error())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer p.Close()

	ticker, err := p.Ticker(ctx)
	if err != nil {
		t.Fatal(err)
	}

	numUpdates := 0
	maxUpdates := 50

	for update := range ticker {
		if update == nil {
			t.Fatal("expected non nil update, got:", update)
		}

		if update.CounterCurrency == "" || update.BaseCurrency == "" {
			t.Error("expected non-empty to/from currency, got:", update.BaseCurrency)
		}

		if update.BaseVolume == 0 || update.DailyHigh == 0 || update.HighestBid == 0 || update.LastRate == 0 ||
			update.LowestAsk == 0 {
			t.Error("expected non-zero currency metrics, got:", update)
		}

		numUpdates++

		if numUpdates >= maxUpdates {
			break
		}
	}

	if numUpdates != maxUpdates {
		t.Fatal("expected", maxUpdates, "updates got:", numUpdates)
	}
}

func TestBalances(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	p, err := poloniex.New()
	if err != nil {
		t.Fatal("encountered an unexpected error:", err.Error())
	}

	key := os.Getenv("POLONIEX_API_KEY")
	secret := os.Getenv("POLONIEX_API_SECRET")

	balances, err := p.Balances(context.Background(), key, secret)
	if err != nil {
		t.Fatal("encountered an unexpected error:", err)
	}

	if balance, ok := balances["BTC"]; !ok {
		if !ok {
			t.Error("unable to find any Bitcoin (BTC) in the balances map!")
		}

		if balance.Available < 0 {
			t.Error("incorrect balance: should have positive bitcoin balance")
		}
	}
}

func TestInvalidKeyErrors(t *testing.T) {
	p, err := poloniex.New()
	if err != nil {
		t.Fatal("encountered an unexpected error:", err.Error())
	}

	var key string
	var secret string

	_, err = p.Balances(context.Background(), key, secret)
	if err == nil {
		t.Fatal("invalid key should return an error!")
	}
}

func TestTradeHistory(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	p, err := poloniex.New()
	if err != nil {
		t.Fatal("encountered an unexpected error:", err.Error())
	}

	key := os.Getenv("POLONIEX_API_KEY")
	secret := os.Getenv("POLONIEX_API_SECRET")

	from, _ := time.Parse(poloniex.DateFormat, "2017-05-08 14:54:55")
	to := time.Now()

	history, err := p.TradeHistory(context.Background(), key, secret, "SC", "BTC", from, to)
	if err != nil {
		t.Fatal("encountered an unexpected error:", err)
	}

	tradeHistory, ok := history["BTC_SC"]

	if !ok {
		t.Fatal("unable to find any Bitcoin (BTC)/Siacoin (SC) trades in the trade history map!")
	}

	if len(tradeHistory.Trades) == 0 {
		t.Error("expected to find at least 1 trade from Bitcoin (BTC) to Siacoin (SC) in our history")
	}

	if tradeHistory.AverageBuyPrice == 0 {
		t.Error("expected to have a proper average buy price on the trade history")
	}

	if tradeHistory.AverageSellPrice == 0 {
		t.Error("expected to have a proper average sell price on the trade history")
	}
}

func TestTradeHistoryMissingPairFetchesAll(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	p, err := poloniex.New()
	if err != nil {
		t.Fatal("encountered an unexpected error:", err.Error())
	}

	key := os.Getenv("POLONIEX_API_KEY")
	secret := os.Getenv("POLONIEX_API_SECRET")

	from, _ := time.Parse(poloniex.DateFormat, "2017-05-08 14:54:55")
	to := time.Now()

	history, err := p.TradeHistory(context.Background(), key, secret, "", "BTC", from, to)
	if err != nil {
		t.Fatal("encountered an unexpected error:", err)
	}

	tradeHistory, ok := history["BTC_SC"]
	if !ok {
		t.Fatal("unable to find any Bitcoin (BTC)/Siacoin (SC) trades in the trade history map!")
	}

	if len(history) <= 1 {
		t.Error("expected to find all trades in the time range")
	}

	if tradeHistory.AverageBuyPrice == 0 {
		t.Error("expected to have a proper average buy price on the trade history")
	}

	if tradeHistory.AverageSellPrice == 0 {
		t.Error("expected to have a proper average sell price on the trade history")
	}
}
