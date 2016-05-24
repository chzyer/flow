package flow

import (
	"errors"
	"sync/atomic"
)

var ErrCanceled = errors.New("operation is canceled")

type Broadcast struct {
	notify atomic.Value
}

func NewBroadcast() *Broadcast {
	b := &Broadcast{}
	b.init()
	return b
}

func (b *Broadcast) init() {
	b.notify.Store(make(chan struct{}))
}

func (b *Broadcast) Wait() chan struct{} {
	return b.notify.Load().(chan struct{})
}

func (b *Broadcast) Notify() {
	ch := b.Wait()
	b.init()
	close(ch)
}

func (b *Broadcast) Close() {
	close(b.Wait())
}
