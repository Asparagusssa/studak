package maxbot

import (
	"context"
	"time"

	"studak/internal/crud"
	"studak/internal/logx"
	"studak/internal/models"
)

func (b *Bot) deliverPendingMatch(a, c uint) error {
	return crud.DeliverPendingMatch(b.DB, a, c, func(recipient, peer *models.User) error {
		_, id, channel := b.destination(recipient)
		if id <= 0 || channel != b.channel() {
			return crud.ErrNotFound
		}
		return b.sendMatch(id, peer)
	})
}

// RunMatchDelivery retries temporary mutuality messages after transport errors
// and restarts. Successful pairs are removed rather than kept as match history.
func RunMatchDelivery(ctx context.Context, bots ...*Bot) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		for _, bot := range bots {
			if bot == nil {
				continue
			}
			current := *bot
			current.ctx = ctx
			current.DB = bot.DB.WithContext(ctx)
			started, err := crud.DatingStarted(current.DB)
			if err != nil {
				if ctx.Err() == nil {
					logx.Error("match delivery launch status", err)
				}
				continue
			}
			if !started {
				continue
			}
			pending, err := crud.PendingMatches(current.DB, current.channel())
			if err != nil {
				logx.Error("pending match deliveries", err)
				continue
			}
			for _, pair := range pending {
				if ctx.Err() != nil {
					return
				}
				if err := current.deliverPendingMatch(pair.FromUserID, pair.ToUserID); err != nil && ctx.Err() == nil {
					logx.Error("mutuality delivery retry", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
