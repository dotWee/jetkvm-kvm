package rdp

import (
	"testing"
	"time"
)

func drainFrames(ch <-chan Frame) int {
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			return count
		}
	}
}

func TestFramePublisherBurstObeysMaxFPS(t *testing.T) {
	p := NewFramePublisher(10)
	id, ch := p.Subscribe(16)
	defer p.Unsubscribe(id)

	frame := Frame{Data: []byte{1, 2, 3}}
	for i := 0; i < 20; i++ {
		p.Publish(frame)
	}

	// Burst should be throttled to the first frame only.
	time.Sleep(20 * time.Millisecond)
	count := drainFrames(ch)
	if count > 1 {
		t.Fatalf("expected throttled burst to send <=1 frame, got %d", count)
	}

	// After one interval we should be able to publish again.
	time.Sleep(120 * time.Millisecond)
	p.Publish(frame)
	time.Sleep(20 * time.Millisecond)
	count += drainFrames(ch)
	if count < 2 {
		t.Fatalf("expected at least 2 frames after waiting for next interval, got %d", count)
	}
}

func TestFramePublisherUnsubscribeStopsDelivery(t *testing.T) {
	p := NewFramePublisher(30)
	id, ch := p.Subscribe(4)
	p.Unsubscribe(id)

	p.Publish(Frame{Data: []byte{1}})
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("expected subscriber channel to be closed after unsubscribe")
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("expected closed channel after unsubscribe")
	}
}
