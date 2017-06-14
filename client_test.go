package poloniex_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"os"

	"github.com/willmadison/poloniex"
)

var p *poloniex.Client

func TestMain(m *testing.M) {
	key := os.Getenv("POLONIEX_API_KEY")
	secret := os.Getenv("POLONIEX_API_SECRET")

	var err error
	p, err = poloniex.New(key, secret)
	if err != nil {
		fmt.Println("encountered unexpected error:", err)
		os.Exit(-1)
	}
	defer p.Close()

	os.Exit(m.Run())
}

func TestTicker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	balances, err := p.Balances(context.Background())
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
	var key string
	var secret string

	var err error
	invalid, err := poloniex.New(key, secret)
	if err != nil {
		t.Fatal("unable to instantiate new Poloniex client:", err)
	}

	_, err = invalid.Balances(context.Background())
	if err == nil {
		t.Fatal("invalid key should return an error!")
	}
}

func TestTradeHistory(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	from, _ := time.Parse(poloniex.DateFormat, "2017-05-08 14:54:55")
	to := time.Now()

	history, err := p.TradeHistory(context.Background(), poloniex.WithCurrencyPair("SC", "BTC"), poloniex.WithTimeFrame(from, to))
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

	from, _ := time.Parse(poloniex.DateFormat, "2017-05-08 14:54:55")
	to := time.Now()

	history, err := p.TradeHistory(context.Background(), poloniex.WithTimeFrame(from, to))
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

func TestTradeHistoryEmptyHistory(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	from := time.Now()
	to, _ := time.Parse(poloniex.DateFormat, "2017-05-08 14:54:55")

	history, err := p.TradeHistory(context.Background(), poloniex.WithTimeFrame(from, to))
	if err != nil {
		t.Fatal("encountered an unexpected error:", err)
	}

	if len(history) != 0 {
		t.Error("expected no trade history in the given time frame")
	}
}

func TestBuyWithInsufficientTransactionTotal(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	_, err := p.Buy(context.Background(), "SC", "BTC", 1, 0.00000500, poloniex.WithPostOnly())
	if err == nil {
		t.Fatal("no error encountered. Expected an error regarding insufficient funds!")
	}
}

func TestBuyWithEnoughBTCValue(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	receipt, err := p.Buy(context.Background(), "SC", "BTC", 50, 0.00000500, poloniex.WithPostOnly())
	if err != nil {
		t.Fatal("unexpected error encountered:", err)
	}

	if receipt.OrderNumber == 0 {
		t.Fatal("expected a valid order number to be returned!")
	}

	err = p.CancelOrder(context.Background(), receipt.OrderNumber)
	if err != nil {
		t.Error("expected order cancellation to be successful")
	}
}

func TestSellWithInsufficientTransactionTotal(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	_, err := p.Sell(context.Background(), "SC", "BTC", 1, 0.00000500, poloniex.WithPostOnly())
	if err == nil {
		t.Fatal("no error encountered. Expected an error regarding insufficient funds!")
	}
}

func TestSellWithEnoughBTCValue(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	receipt, err := p.Sell(context.Background(), "SC", "BTC", 50, 0.00000500, poloniex.WithPostOnly())
	if err != nil {
		t.Fatal("unexpected error encountered:", err)
	}

	if receipt.OrderNumber == 0 {
		t.Fatal("expected a valid order number to be returned!")
	}

	err = p.CancelOrder(context.Background(), receipt.OrderNumber)
	if err != nil {
		t.Error("expected order cancellation to be successful")
	}
}

func TestFeeSchedule(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	schedule, err := p.FeeSchedule(context.Background())
	if err != nil {
		t.Fatal("unexpected error encountered:", err)
	}

	if schedule.MakerFee == 0 {
		t.Error("expected non-zero makerFee")
	}

	if schedule.TakerFee == 0 {
		t.Error("expected non-zero takerFee")
	}

	if schedule.ThirtyDayVolume == 0 {
		t.Error("expected non-zero thirtyDayVolume")
	}
}

func TestOrderCancellation(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip()
	}

	receipt, err := p.Sell(context.Background(), "SC", "BTC", 50, 0.00000500, poloniex.WithPostOnly())
	if err != nil {
		t.Fatal("unexpected error encountered:", err)
	}

	if receipt.OrderNumber == 0 {
		t.Fatal("expected a valid order number to be returned!")
	}

	err = p.CancelOrder(context.Background(), receipt.OrderNumber)
	if err != nil {
		t.Error("expected order cancellation to be successful")
	}
}
