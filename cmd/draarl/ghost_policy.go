package main

import (
	"draarl/internal/config"
	"draarl/internal/ghostsession"
)

func ghostSessionPolicy(cfg config.GhostSessionConfig) ghostsession.Policy {
	return ghostsession.Policy{
		MultiSession: ghostsession.NewFeatureGate(
			cfg.MultiSession.IsEnabled(), cfg.MultiSession.AllowOwnerIDs, cfg.MultiSession.AllowDevModels,
		),
		MultiReceive: ghostsession.NewFeatureGate(
			cfg.MultiReceive.IsEnabled(), cfg.MultiReceive.AllowOwnerIDs, cfg.MultiReceive.AllowDevModels,
		),
	}
}
