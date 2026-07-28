package httpapi

import (
	"context"
	"time"
)

// syncGameObjects mirrors only the namespaces a mutation may have changed.
// The local asset transaction remains authoritative and Caddy continues to
// serve it, so a transient mirror failure never turns a successful catalog
// mutation into a misleading database failure. Startup performs a full
// reconciliation before accepting traffic, and each later mutation retries
// the affected prefixes immediately.
func (s *Server) syncGameObjects(prefixes ...string) {
	if s.assets == nil || s.assets.Provider() == "local" || len(prefixes) == 0 {
		return
	}
	timeout := s.config.ObjectStorageSyncTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = s.assets.Sync(ctx, s.config.AssetRoot, prefixes...)
		if err == nil || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(attempt+1) * 150 * time.Millisecond):
		}
	}
	if err != nil {
		s.logger.Error("object storage mirror requires reconciliation", "provider", s.assets.Provider(), "prefixes", prefixes, "error", err)
	}
}
