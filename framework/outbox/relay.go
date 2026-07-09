package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// StartRelay launches the Relay goroutine. The relay delivers each pending
// outbox row to every declared durable consumer INDEPENDENTLY: it expands
// per-consumer delivery rows, claims a batch, invokes each consumer's
// handler directly (the relay no longer touches the live event bus), and
// settles the delivery — dispatched on success, retried with backoff on
// error or panic, dead after MaxAttempts. A parent row flips to dispatched
// once it has no pending deliveries left.
//
// The loop runs until ctx is cancelled. The returned stop func blocks
// until the loop has fully exited, so callers can drain safely on
// shutdown. Register all consumers via [Outbox.Consume] before calling
// this; the declared set is read once per pump and must not change after
// the relay starts.
func (o *Outbox) StartRelay(ctx context.Context) (stop func()) {
	if !o.hasConsumers() {
		// Zero declared consumers means the durable lane is a no-op: staged
		// rows get no deliveries and are dropped by the orphan sweep once
		// they age past the handler grace. That is almost certainly a
		// misconfiguration, so surface it loudly once at start rather than
		// silently swallowing events.
		slog.Default().Warn("outbox: relay started with no declared consumers; staged events will be dropped — declare consumers via Consume / framework.WithOutboxConsumer",
			"table", o.table)
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		o.relayLoop(ctx, stopCh)
	}()
	return func() {
		select {
		case <-stopCh:
		default:
			close(stopCh)
		}
		<-doneCh
	}
}

// Nudge wakes the Relay immediately (non-blocking send on a cap-1
// channel). Callers invoke it right after commit so delivery latency is
// not bound to PollInterval. Extra nudges coalesce — only one wake is
// buffered regardless of how many arrive between pumps.
func (o *Outbox) Nudge() {
	select {
	case o.nudge <- struct{}{}:
	default:
	}
}

func (o *Outbox) relayLoop(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()
	for {
		// A full batch may mean a backlog — drain immediately instead
		// of waiting for the next tick. Still honour stop/ctx between
		// pumps: without this check a sustained backlog keeps n > 0
		// forever and stop() would block until the backlog drains.
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

// pump runs one relay cycle: reconcile the declared-consumer set against
// the table (expand + retire + orphan sweep), then claim a batch of
// deliveries and settle each synchronously. Returns the number of
// deliveries processed so the loop can decide whether to drain.
func (o *Outbox) pump(ctx context.Context) int {
	// Phase 1 — reconcile. Any failure here (missing/renamed table under
	// WithoutEnsureTable, a mid-run DB outage) stalls delivery; the relay
	// tolerates it by continuing to poll, but logs once on onset (and once
	// on recovery) so a stalled relay is observable without flooding.
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
	for _, d := range deliveries {
		if ctx.Err() != nil {
			// Shutdown mid-batch: claimed deliveries stay leased and are
			// recovered after the lease expires (at-least-once).
			break
		}
		o.processDelivery(ctx, d)
	}
	return len(deliveries)
}

// processDelivery invokes one claimed delivery's consumer handler and
// settles the delivery row, then attempts to complete the parent.
func (o *Outbox) processDelivery(ctx context.Context, d claimedDelivery) {
	// Settle/complete writes must survive shutdown: the handler may already
	// have run to completion, so its outcome must be recorded durably even if
	// the relay ctx is being cancelled — otherwise a delivery that SUCCEEDED
	// is left pending and needlessly redelivered after the lease expires. The
	// handler itself still runs under ctx (a cooperative handler stops on
	// shutdown); only the bookkeeping writes are detached, bounded so a dead
	// DB can't hang shutdown.
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	handler, ok := o.lookupHandler(d.Type, d.Consumer)
	if !ok {
		// Declared on another replica mid rolling-deploy: leave it pending
		// with a short backoff and log; never dead-letter — another
		// replica may hold the handler.
		slog.Default().Debug("outbox: delivery has no handler on this replica; requeued",
			"row_id", d.RowID, "consumer", d.Consumer, "type", d.Type)
		o.requeueNoHandler(settleCtx, d)
		return
	}
	var payload map[string]any
	if len(d.Payload) > 0 {
		if err := json.Unmarshal(d.Payload, &payload); err != nil {
			o.markDeliveryFailure(settleCtx, d, fmt.Errorf("unmarshal payload: %w", err))
			o.completeParent(settleCtx, d.RowID)
			return
		}
	}
	ev := event.Event{
		ID:        d.RowID,
		Type:      d.Type,
		Data:      payload,
		Timestamp: d.CreatedAt,
	}
	// invokeHandler recovers a panic into a delivery error, so a panicking
	// consumer is retried/dead-lettered — never silently marked dispatched.
	if err := invokeHandler(ctx, handler, ev, d.Consumer); err != nil {
		o.markDeliveryFailure(settleCtx, d, err)
		o.completeParent(settleCtx, d.RowID)
		return
	}
	o.markDeliveryDispatched(settleCtx, d)
	o.completeParent(settleCtx, d.RowID)
}

// logClaimErr records a claim/reconcile failure once on onset. pump runs
// only on the single relay goroutine, so claimErrLogged needs no lock.
func (o *Outbox) logClaimErr(_ context.Context, what string, err error) {
	if !o.claimErrLogged {
		slog.Default().Error("outbox: relay "+what+" failed; delivery is stalled until this clears",
			"table", o.table, "err", err)
		o.claimErrLogged = true
	}
}

// clearClaimErr logs the recovery once after a prior run of failures.
func (o *Outbox) clearClaimErr() {
	if o.claimErrLogged {
		slog.Default().Info("outbox: relay recovered; delivery resumed", "table", o.table)
		o.claimErrLogged = false
	}
}
