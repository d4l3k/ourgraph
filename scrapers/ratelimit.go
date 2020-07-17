package scrapers

import (
	"context"
	"log"
	"time"

	"go.uber.org/ratelimit"
	"golang.org/x/time/rate"
)

type LimiterWrapper struct {
	Limiter *rate.Limiter
}

func (l LimiterWrapper) Take() time.Time {
	if err := l.Limiter.Wait(context.Background()); err != nil {
		log.Fatalf("%+v", err)
	}
	return time.Now()
}

var _ ratelimit.Limiter = LimiterWrapper{}
