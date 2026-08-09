package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/repository"
	schedruntime "draarl/internal/broadcast/scheduler/runtime"
	"draarl/internal/config"
	"draarl/pkg/storage"
)

var ErrUnavailable = errors.New("broadcast scheduler is unavailable")

var global struct {
	sync.RWMutex
	engine *schedruntime.Engine
}

func Init(cfg *config.Configuration) error {
	if cfg == nil || !cfg.Broadcast.Enabled {
		return nil
	}
	if !storage.IsEnabled() {
		return fmt.Errorf("broadcast scheduler requires initialized storage")
	}
	global.Lock()
	defer global.Unlock()
	if global.engine != nil {
		return nil
	}
	engine, err := schedruntime.NewEngine(cfg.Broadcast, repository.Default(), storage.Get())
	if err != nil {
		return err
	}
	if err := engine.Start(); err != nil {
		return err
	}
	global.engine = engine
	return nil
}

func CancelRun(runID uint, cause error) bool {
	global.RLock()
	engine := global.engine
	global.RUnlock()
	return engine != nil && engine.CancelRun(runID, cause)
}

func TriggerManual(ctx context.Context, groupID int, scheduleID uint) (*model.BroadcastRun, string, error) {
	global.RLock()
	engine := global.engine
	global.RUnlock()
	if engine == nil {
		return nil, "", ErrUnavailable
	}
	return engine.TriggerManual(ctx, groupID, scheduleID)
}

func CancelGroups(groupIDs []int, cause error) int {
	global.RLock()
	engine := global.engine
	global.RUnlock()
	if engine == nil {
		return 0
	}
	return engine.CancelGroups(groupIDs, cause)
}

func CancelGroupsAndWait(ctx context.Context, groupIDs []int, cause error) (int, error) {
	global.RLock()
	engine := global.engine
	global.RUnlock()
	if engine == nil {
		return 0, nil
	}
	return engine.CancelGroupsAndWait(ctx, groupIDs, cause, true)
}

func CancelRunsAndWait(ctx context.Context, runIDs []uint, cause error) (int, error) {
	global.RLock()
	engine := global.engine
	global.RUnlock()
	if engine == nil {
		return 0, nil
	}
	return engine.CancelRunsAndWait(ctx, runIDs, cause)
}

func Stop(ctx context.Context) error {
	global.Lock()
	engine := global.engine
	global.engine = nil
	global.Unlock()
	if engine == nil {
		return nil
	}
	err := engine.Stop(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
