package flow

import (
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"gopkg.in/logex.v1"
)

type Flow struct {
	Debug    *bool
	errChan  chan error
	stopChan chan struct{}
	ref      *int32
	wg       sync.WaitGroup
	Parent   *Flow
	Children []*Flow
	stoped   int32
	onClose  func()

	mutex sync.Mutex
}

func New(n int) *Flow {
	debug := false
	f := &Flow{
		Debug:    &debug,
		errChan:  make(chan error, 1),
		stopChan: make(chan struct{}),
		ref:      new(int32),
	}
	f.Add(n)
	return f
}

func (f *Flow) SetOnClose(exit func()) {
	f.onClose = exit
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
	f2.Debug = f.Debug
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
	if *f.Debug {
		logex.DownLevel(1).Info("close")
	}
	f.close()
}

func (f *Flow) close() {
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
	if f.onClose != nil {
		f.onClose()
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
	if *f.Debug {
		logex.DownLevel(1).Info("add:", n, "ref:", *f.ref)
	}
	f.wg.Add(n)
}

func (f *Flow) Done() {
	f.wg.Done()
	if atomic.AddInt32(f.ref, -1) == 0 {
		f.Stop()
	}
}

func (f *Flow) DoneAndClose() {
	if *f.Debug {
		logex.DownLevel(1).Info("done and close, ref:", *f.ref)
	}
	f.Done()
	f.close()
}

func (f *Flow) wait() {
	<-f.stopChan
	f.wg.Wait()

	if f.Parent != nil {
		f.Parent.Done()
		f.Parent = nil
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
