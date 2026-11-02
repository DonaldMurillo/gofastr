// Package gmfixed is core/mcp after d3cfff016: the per-caller record is
// capped across every caller before the insert.
package gmfixed

import (
	"context"
	"errors"
)

type resourceSubKey struct{ caller, uri string }

type ctxKey struct{}

func subscriptionCaller(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}

const maxResourceSubscriptionPairs = 4096

type Server struct {
	resourceSubsBy map[resourceSubKey]int
}

func (s *Server) handleResourcesSubscribe(ctx context.Context, uri string) error {
	key := resourceSubKey{caller: subscriptionCaller(ctx), uri: uri}
	if _, exists := s.resourceSubsBy[key]; !exists && len(s.resourceSubsBy) >= maxResourceSubscriptionPairs {
		return errors.New("too many resource subscriptions")
	}
	s.resourceSubsBy[key]++
	return nil
}
