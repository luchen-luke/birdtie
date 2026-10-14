package main

import (
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/systempush"
)

func loadSystemPushStartup(store *postgres.Store, lookup func(string) (string, bool)) (systempush.HumanGateway, error) {
	cfg, e := systempush.LoadConfig(lookup)
	if e != nil {
		return nil, e
	}
	var ring *systempush.Keyring
	if cfg.Enabled {
		ring, e = systempush.LoadKeyringFile(cfg.KeyringFile)
		if e != nil {
			return nil, e
		}
	}
	// The API manages bindings; only the separately configured bounded worker
	// constructs providers. Startup never requests a credential or sends a push.
	return postgres.NewSystemPush(store, ring, cfg)
}
