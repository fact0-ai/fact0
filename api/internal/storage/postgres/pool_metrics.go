package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fact0-ai/fact0/internal/platform/metrics"
)

// StartPoolMetrics exports pgx pool stats on an interval until ctx is cancelled.
func StartPoolMetrics(ctx context.Context, writePool, readPool *pgxpool.Pool) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	export := func(name string, p *pgxpool.Pool) {
		if p == nil {
			return
		}
		s := p.Stat()
		metrics.DBPoolConnections.WithLabelValues(name, "total").Set(float64(s.TotalConns()))
		metrics.DBPoolConnections.WithLabelValues(name, "idle").Set(float64(s.IdleConns()))
		metrics.DBPoolConnections.WithLabelValues(name, "acquired").Set(float64(s.AcquiredConns()))
		metrics.DBPoolConnections.WithLabelValues(name, "max").Set(float64(s.MaxConns()))
	}
	for {
		export("write", writePool)
		export("read", readPool)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
