package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// StartRelay starts the relay goroutine, which delivers every pending row to
// each declared consumer until ctx ends. The returned stop blocks until the
// loop has exited. Declare every consumer with [Outbox.Consume] first.
func (o *Outbox) StartRelay(ctx context.Context) (stop func()) {
	if !o.hasConsumers() {
		slog.Default().Warn("outbox: relay started with no declared consumers; staged events will be dropped: declare consumers via Consume / framework.WithOutboxConsumer",
			"table", o.table)
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		o.relayLoop(ctx, stopCh)
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(stopCh) })
		<-doneCh
	}
}

// Nudge wakes the relay at once, so a row committed now need not wait for the
// poll interval. Nudges that arrive between pumps coalesce into one.
func (o *Outbox) Nudge() {
	select {
	case o.nudge <- struct{}{}:
	default:
	}
}

func (o *Outbox) relayLoop(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()
	if err := o.normalizeLegacyTimestamps(ctx); err != nil {
		slog.Default().Error("outbox: legacy timestamp normalization failed; delivery stalled until this clears",
			"table", o.table, "err", err)
		o.claimErrLogged = true
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		default:
		}
		n := o.pump(ctx)
		if n == 0 {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-o.nudge:
			case <-ticker.C:
			}
		}
	}
}

func (o *Outbox) pump(ctx context.Context) int {
	if _, err := o.expandDeliveries(ctx); err != nil {
		o.logClaimErr(ctx, "expand deliveries", err)
		return 0
	}
	if err := o.sweepParents(ctx); err != nil {
		o.logClaimErr(ctx, "sweep parents", err)
		return 0
	}
	if err := o.purgeExpired(ctx); err != nil {
		o.logClaimErr(ctx, "purge expired", err)
		return 0
	}
	deliveries, err := o.claimDeliveries(ctx)
	if err != nil {
		o.logClaimErr(ctx, "claim deliveries", err)
		return 0
	}
	o.clearClaimErr()
	if len(deliveries) == 0 {
		return 0
	}
	release := func(rest []claimedDelivery) {
		settleCtx, cancel := o.settleContext(ctx)
		o.releaseUnstarted(settleCtx, rest)
		cancel()
	}
	for i, d := range deliveries {
		if ctx.Err() != nil {
			release(deliveries[i:])
			break
		}
		if i > 0 && o.now().UTC().Add(o.handlerTimeout+o.settleTimeout).After(d.ClaimedUntil) {
			release(deliveries[i:])
			break
		}
		o.processDelivery(ctx, d)
	}
	return len(deliveries)
}

func (o *Outbox) processDelivery(ctx context.Context, d claimedDelivery) {
	handler, ok := o.lookupHandler(d.Type, d.Consumer)
	if !ok {
		slog.Default().Debug("outbox: delivery has no handler on this replica; requeued",
			"row_id", d.RowID, "consumer", d.Consumer, "type", d.Type)
		settleCtx, cancel := o.settleContext(ctx)
		defer cancel()
		o.requeueNoHandler(settleCtx, d)
		return
	}
	var payload any
	if len(d.Payload) > 0 {
		if err := jsonexact.UnmarshalFullRange(d.Payload, &payload); err != nil {
			settleCtx, cancel := o.settleContext(ctx)
			defer cancel()
			o.markDeliveryFailure(settleCtx, d, fmt.Errorf("unmarshal payload: %w", err))
			o.completeParent(settleCtx, d.RowID, d.Type)
			return
		}
	}
	ev := event.Event{
		ID:        d.RowID,
		Type:      d.Type,
		Data:      payload,
		Timestamp: d.CreatedAt,
	}
	herr := o.runHandler(ctx, handler, ev, d)
	settleCtx, cancel := o.settleContext(ctx)
	defer cancel()
	if herr != nil && ctx.Err() != nil {
		slog.Default().Info("outbox: shutdown interrupted a handler; delivery handed back",
			"row_id", d.RowID, "consumer", d.Consumer, "type", d.Type, "err", herr)
		o.releaseUnstarted(settleCtx, []claimedDelivery{d})
		return
	}
	if herr != nil {
		o.markDeliveryFailure(settleCtx, d, herr)
		o.completeParent(settleCtx, d.RowID, d.Type)
		return
	}
	o.markDeliveryDispatched(settleCtx, d)
	o.completeParent(settleCtx, d.RowID, d.Type)
}

func (o *Outbox) settleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), o.settleTimeout)
}

func (o *Outbox) runHandler(ctx context.Context, h event.EventHandler, ev event.Event, d claimedDelivery) error {
	hctx, hcancel := context.WithTimeout(ctx, o.handlerTimeout)
	defer hcancel()
	type outcome struct{ err error }
	done := make(chan outcome, 1)
	go func() { done <- outcome{invokeHandler(hctx, h, ev, d.Consumer)} }()
	timer := time.NewTimer(o.handlerTimeout)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.err
	case <-timer.C:
		return fmt.Errorf("outbox: consumer %q for %q handler exceeded its %s budget (context cancelled; delivery retried)",
			d.Consumer, d.Type, o.handlerTimeout)
	}
}

func (o *Outbox) logClaimErr(_ context.Context, what string, err error) {
	if !o.claimErrLogged {
		slog.Default().Error("outbox: relay "+what+" failed; delivery is stalled until this clears",
			"table", o.table, "err", err)
		o.claimErrLogged = true
	}
}

func (o *Outbox) clearClaimErr() {
	if o.claimErrLogged {
		slog.Default().Info("outbox: relay recovered; delivery resumed", "table", o.table)
		o.claimErrLogged = false
	}
}
