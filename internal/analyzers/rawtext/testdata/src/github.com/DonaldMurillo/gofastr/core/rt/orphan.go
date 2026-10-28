package rt

import "context"

type blockConfig struct {
	Ctx  context.Context
	Code string
}

func block(cfg blockConfig) string {
	_ = cfg.Ctx
	return cfg.Code
}

// orphanOutsideKit: the no-context arm reads kit packages only.
func orphanOutsideKit(code string) string {
	return block(blockConfig{Code: code})
}
