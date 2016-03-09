package flow

import (
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Flow struct {
	errChan  chan error
	stopChan chan struct{}
	ref      *int32
	wg       sync.WaitGroup
	Parent   *Flow
	Children []*Flow
	stoped   int32

	mutex sync.Mutex
}

func New(n int) *Flow {
	f := &Flow{
		errChan:  make(chan error, 1),
		stopChan: make(chan struct{}),
		ref:      new(int32),
	}
	f.Add(n)
	return f
}

const (
	F_CLOSED  = true
	F_TIMEOUT = false
)

func (f *Flow) CloseOrWait(duration time.Duration) bool {
	select {
	case <-time.After(duration):
		return F_TIMEOUT
	case <-f.IsClose():
		return F_CLOSED
	}
}

func (f *Flow) Error(err error) {
	f.errChan <- err
}

func (f *Flow) Fork(n int) *Flow {
	f2 := New(n)
	f2.Parent = f
	f.Children = append(f.Children, f2)
	f.Add(1) // for f2
	return f2
}

func (f *Flow) StopAll() {
	flow := f
	for flow.Parent != nil {
		flow = flow.Parent
	}
	flow.Stop()
}

func (f *Flow) Close() {
	f.Stop()
	f.wait()
}

func (f *Flow) Stop() {
	if !atomic.CompareAndSwapInt32(&f.stoped, 0, 1) {
		return
	}

	close(f.stopChan)
	for _, cf := range f.Children {
		cf.Stop()
	}
}

func (f *Flow) IsClosed() bool {
	return atomic.LoadInt32(&f.stoped) == 1
}

func (f *Flow) IsClose() chan struct{} {
	return f.stopChan
}

func (f *Flow) Add(n int) {
	atomic.AddInt32(f.ref, int32(n))
	f.wg.Add(n)
}

func (f *Flow) Done() {
	f.wg.Done()
	if atomic.AddInt32(f.ref, -1) == 0 {
		f.Stop()
	}
}

func (f *Flow) wait() {
	<-f.stopChan
	f.wg.Wait()

	if f.Parent != nil {
		f.Parent.Done()
	}
}

func (f *Flow) Wait() error {
	signalChan := make(chan os.Signal)
	signal.Notify(signalChan,
		os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGHUP)
	var err error
	select {
	case <-f.IsClose():
	case <-signalChan:
		f.Stop()
	case err = <-f.errChan:
		if err != nil {
			f.Stop()
		}
	}
	f.wait()
	return err
}
