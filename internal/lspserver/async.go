package lspserver

import (
	"context"
	"log"
	"time"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/processor"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
)

func (s *Server) runAsyncChecks(
	ctx context.Context,
	filePath string,
	content []byte,
	cfg *config.Config,
	fastViolations []rules.Violation,
	plans []async.CheckRequest,
) *async.RunResult {
	if cfg == nil || len(plans) == 0 || !config.SlowChecksEnabled(cfg.SlowChecks.Mode) {
		return nil
	}

	procCtx := processor.NewContext(
		map[string]*config.Config{filePath: cfg},
		cfg,
		map[string][]byte{filePath: content},
	)
	filtered := processor.NewSeverityOverride().Process(fastViolations, procCtx)
	filtered = processor.NewEnableFilter().Process(filtered, procCtx)
	if cfg.SlowChecks.FailFast && hasSeverityError(filtered) {
		return nil
	}

	timeout := 20 * time.Second
	if cfg.SlowChecks.Timeout != "" {
		d, err := time.ParseDuration(cfg.SlowChecks.Timeout)
		if err != nil {
			log.Printf("lsp: invalid slow-checks.timeout %q for %s: %v", cfg.SlowChecks.Timeout, filePath, err)
		} else if d > 0 {
			timeout = d
		}
	}

	if registry.NewDefaultResolver == nil {
		log.Printf("lsp: slow checks unavailable for %s (missing build tags)", filePath)
		return nil
	}

	imgResolver := registry.NewDefaultResolver()
	asyncImgResolver := registry.NewAsyncImageResolver(imgResolver)
	rt := &async.Runtime{
		Concurrency: 4,
		Timeout:     timeout,
		Resolvers: map[string]async.Resolver{
			asyncImgResolver.ID(): asyncImgResolver,
		},
	}

	return rt.Run(ctx, plans)
}

func hasSeverityError(violations []rules.Violation) bool {
	for _, v := range violations {
		if v.Severity == rules.SeverityError {
			return true
		}
	}
	return false
}
