package media

import (
	"context"
	"fmt"
	"sync"

	"draarl/internal/broadcast/repository"
	"draarl/internal/config"
	"draarl/pkg/storage"
)

var (
	processorMu     sync.RWMutex
	globalProcessor *Processor
)

func InitProcessor(cfg *config.Configuration) error {
	if cfg == nil || !cfg.Broadcast.Enabled {
		return nil
	}
	if !storage.IsEnabled() {
		return fmt.Errorf("broadcast media processor requires initialized storage")
	}
	processor := NewProcessor(cfg.Broadcast, repository.Default())
	if err := processor.Start(); err != nil {
		return err
	}
	processorMu.Lock()
	globalProcessor = processor
	processorMu.Unlock()
	return nil
}

func Enqueue(audioID uint) error {
	processorMu.RLock()
	processor := globalProcessor
	processorMu.RUnlock()
	if processor == nil {
		return fmt.Errorf("broadcast media processor is not running")
	}
	return processor.Enqueue(audioID)
}

func GetMetrics() MetricsSnapshot {
	processorMu.RLock()
	processor := globalProcessor
	processorMu.RUnlock()
	if processor == nil {
		return MetricsSnapshot{}
	}
	return processor.Metrics()
}

func StopProcessor(ctx context.Context) error {
	processorMu.Lock()
	processor := globalProcessor
	globalProcessor = nil
	processorMu.Unlock()
	if processor == nil {
		return nil
	}
	return processor.Stop(ctx)
}
