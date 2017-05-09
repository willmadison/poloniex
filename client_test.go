package poloniex_test

import (
	"context"
	"testing"

	"github.com/willmadison/poloniex"
)

func TestTicker(t *testing.T) {
	p, err := poloniex.New()

	if err != nil {
		t.Fatal("encountered an unexpected error:", err.Error())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := p.Ticker(ctx)

	for i := 0; i < 50; i++ {
		update := <-ticker

		if update == nil {
			t.Fatal("expected non nil update, got:", update)
		}

		if update.ToCurrency == "" || update.FromCurrency == "" {
			t.Error("expected non-empty to/from currency, got:", update.FromCurrency)
		}

		if update.BaseVolume == 0.0 || update.DailyHigh == 0 || update.HighestBid == 0 || update.LastRate == 0 ||
			update.LowestAsk == 0.0 || update.PercentageChange == 0.0 {
			t.Error("expected non-zero currency metrics, got:", update)
		}
	}
}
