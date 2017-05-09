package poloniex_test

import (
	"context"
	"testing"

	"github.com/willmadison/poloniex"
)

func TestTicker(t *testing.T) {
	p, err := poloniex.New()

	if err != nil {
		t.Error("encountered an unexpected error:", err.Error())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := p.Ticker(ctx)

	update := <-ticker

	if update == nil {
		t.Error("expected non nil update, got:", update)
	}
}
