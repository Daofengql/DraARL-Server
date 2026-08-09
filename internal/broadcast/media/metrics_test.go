package media

import (
	"testing"
	"time"
)

func TestProcessorMetricsTracksQueueAndTranscodeOutcomes(t *testing.T) {
	processor := &Processor{jobs: make(chan uint, 4)}
	processor.metrics.running.Store(true)
	processor.jobs <- 10
	processor.metrics.enqueued.Add(1)

	startedAt := processor.metrics.begin()
	time.Sleep(time.Millisecond)
	processor.metrics.finish(startedAt, true)
	startedAt = processor.metrics.begin()
	processor.metrics.finish(startedAt, false)

	snapshot := processor.Metrics()
	if !snapshot.Running || snapshot.QueueDepth != 1 || snapshot.QueueCapacity != 4 || snapshot.Enqueued != 1 {
		t.Fatalf("queue metrics=%#v", snapshot)
	}
	if snapshot.Started != 2 || snapshot.Succeeded != 1 || snapshot.Failed != 1 || snapshot.Current != 0 {
		t.Fatalf("outcome metrics=%#v", snapshot)
	}
	if snapshot.TranscodeSamples != 2 || snapshot.TranscodeMaxMS < snapshot.TranscodeLastMS {
		t.Fatalf("duration metrics=%#v", snapshot)
	}
}
