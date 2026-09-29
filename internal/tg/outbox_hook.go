package tg

import (
	"context"
	"fmt"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/store"
)

// outboxHook sits between the raw MTProto layer and the updates.Manager.
// It extracts UpdateReadHistoryOutbox / UpdateReadChannelOutbox directly from
// the wire update before pts-tracking sees it. updates.Manager drops these
// events when a pts gap exists (the pending buffer never flushes), so we must
// intercept them here to guarantee delivery. Being the one place that sees an
// envelope whole, it also settles which of two updates about one message's
// reactions decides (yieldPairedEditReactions).
//
// Upstream issue: gotd/td#1853, the same buffer discard as common_diff.go
// works around. Whether its fix covers a read the difference never replays is
// an open question; docs/gotd-workarounds.md holds the answer when it arrives.
type outboxHook struct {
	next        telegram.UpdateHandler
	mustDeliver chan<- store.Event
	log         *zap.Logger
}

func newOutboxHook(next telegram.UpdateHandler, mustDeliver chan<- store.Event, log *zap.Logger) *outboxHook {
	return &outboxHook{next: next, mustDeliver: mustDeliver, log: log}
}

func (h *outboxHook) Handle(ctx context.Context, u tg.UpdatesClass) error {
	h.logArrival(u)
	h.extractOutboxReads(ctx, u)
	yieldPairedEditReactions(h.log, u)
	return h.next.Handle(ctx, u)
}

// logArrival records every raw update envelope reaching the client, before the
// pts-tracking layer. This is the top boundary of the update pipeline: it tells
// us whether the server still pushes updates after a long idle (#119) or the
// stream goes silent. Only update *types* are logged here, never peer IDs or
// message content, so it is safe at plain debug level.
func (h *outboxHook) logArrival(u tg.UpdatesClass) {
	ce := h.log.Check(zap.DebugLevel, "update envelope received")
	if ce == nil {
		return
	}
	var inner []string
	switch u := u.(type) {
	case *tg.Updates:
		inner = updateTypeNames(u.Updates)
	case *tg.UpdatesCombined:
		inner = updateTypeNames(u.Updates)
	case *tg.UpdateShort:
		inner = []string{fmt.Sprintf("%T", u.Update)}
	}
	ce.Write(
		zap.String("envelope", fmt.Sprintf("%T", u)),
		zap.Strings("updates", inner),
	)
}

func updateTypeNames(upds []tg.UpdateClass) []string {
	names := make([]string, len(upds))
	for i, u := range upds {
		names[i] = fmt.Sprintf("%T", u)
	}
	return names
}

func (h *outboxHook) extractOutboxReads(ctx context.Context, u tg.UpdatesClass) {
	var upds []tg.UpdateClass
	switch u := u.(type) {
	case *tg.Updates:
		upds = u.Updates
	case *tg.UpdatesCombined:
		upds = u.Updates
	case *tg.UpdateShort:
		upds = []tg.UpdateClass{u.Update}
	default:
		return
	}
	for _, upd := range upds {
		switch upd := upd.(type) {
		case *tg.UpdateReadHistoryOutbox:
			chatID := peerIDFromPeer(upd.Peer)
			if chatID == 0 {
				continue
			}
			h.log.Debug("outboxHook: ReadHistoryOutbox", zap.Int64("chat_id", chatID), zap.Int("max_id", upd.MaxID))
			select {
			case h.mustDeliver <- store.Event{Kind: store.EventReadOutbox, ChatID: chatID, ReadMaxID: upd.MaxID}:
			case <-ctx.Done():
				return
			}
		case *tg.UpdateReadChannelOutbox:
			h.log.Debug("outboxHook: ReadChannelOutbox", zap.Int64("channel_id", upd.ChannelID), zap.Int("max_id", upd.MaxID))
			select {
			case h.mustDeliver <- store.Event{Kind: store.EventReadOutbox, ChatID: upd.ChannelID, ReadMaxID: upd.MaxID}:
			case <-ctx.Done():
				return
			}
		}
	}
}
