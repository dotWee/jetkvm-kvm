package rdp

import (
	"sync"
	"sync/atomic"
	"time"
)

// Frame represents a video frame payload that can be forwarded to RDP clients.
type Frame struct {
	Data     []byte
	Duration time.Duration
}

// FramePublisher throttles outbound frame fan-out to max FPS.
type FramePublisher struct {
	mu          sync.RWMutex
	subscribers map[uint64]chan Frame
	nextID      uint64
	closed      bool

	maxFPS       atomic.Int32
	lastPublishN atomic.Int64
}

func NewFramePublisher(maxFPS int) *FramePublisher {
	norm := NormalizeConfig(Config{MaxFPS: maxFPS})
	p := &FramePublisher{
		subscribers: make(map[uint64]chan Frame),
	}
	p.maxFPS.Store(int32(norm.MaxFPS))
	return p
}

func (p *FramePublisher) SetMaxFPS(maxFPS int) {
	norm := NormalizeConfig(Config{MaxFPS: maxFPS})
	p.maxFPS.Store(int32(norm.MaxFPS))
}

func (p *FramePublisher) MaxFPS() int {
	return int(p.maxFPS.Load())
}

func (p *FramePublisher) Subscribe(buffer int) (uint64, <-chan Frame) {
	if buffer < 1 {
		buffer = 1
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		ch := make(chan Frame)
		close(ch)
		return 0, ch
	}

	p.nextID++
	id := p.nextID
	ch := make(chan Frame, buffer)
	p.subscribers[id] = ch
	return id, ch
}

func (p *FramePublisher) Unsubscribe(id uint64) {
	p.mu.Lock()
	ch, ok := p.subscribers[id]
	if ok {
		delete(p.subscribers, id)
	}
	p.mu.Unlock()

	if ok {
		close(ch)
	}
}

func (p *FramePublisher) SubscriberCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.subscribers)
}

func (p *FramePublisher) Publish(frame Frame) {
	if frame.Data == nil {
		return
	}

	if p.SubscriberCount() == 0 {
		return
	}

	intervalNs := int64(time.Second / time.Duration(p.maxFPS.Load()))
	now := time.Now().UnixNano()
	for {
		last := p.lastPublishN.Load()
		if now-last < intervalNs {
			return
		}
		if p.lastPublishN.CompareAndSwap(last, now) {
			break
		}
	}

	out := make([]byte, len(frame.Data))
	copy(out, frame.Data)
	frame.Data = out

	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, ch := range p.subscribers {
		select {
		case ch <- frame:
		default:
			// Drop frame for slow subscribers.
		}
	}
}

func (p *FramePublisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	for id, ch := range p.subscribers {
		delete(p.subscribers, id)
		close(ch)
	}
}
