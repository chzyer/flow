package flow

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"path"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	pkgPath      = reflect.TypeOf(Flow{}).PkgPath()
	DefaultDebug = false
)

type debugInfo struct {
	Stack string
	Info  string
}

func (d *debugInfo) String() string {
	return d.Stack + " - " + d.Info
}

type Flow struct {
	errChan  chan error
	stopChan chan struct{}
	ref      *int32
	wg       sync.WaitGroup
	Parent   *Flow
	Children []*Flow
	stoped   int32
	exited   int32
	onClose  []func()
	id       uintptr

	mutex   sync.Mutex
	debug   []debugInfo
	printed int32
}

func NewEx(n int) *Flow {
	f := &Flow{
		errChan:  make(chan error, 1),
		stopChan: make(chan struct{}),
		ref:      new(int32),
	}
	f.appendDebug("init")
	return f
}

func New() *Flow {
	return NewEx(0)
}

func (f *Flow) MarkExit() bool {
	return atomic.CompareAndSwapInt32(&f.exited, 0, 1)
}

func (f *Flow) printDebug() {
	buf := bytes.NewBuffer(nil)
	maxLength := 0
	for _, d := range f.debug {
		if maxLength < len(d.Stack) {
			maxLength = len(d.Stack)
		}
	}
	fill := func(a string, n int) string {
		return a + strings.Repeat(" ", n-len(a))
	}
	buf.WriteString("\n")
	for _, d := range f.debug {
		buf.WriteString(fmt.Sprint(&f) + " ")
		buf.WriteString(fill(d.Stack, maxLength) + " - " + d.Info + "\n")
	}
	print(buf.String())
}

func (f *Flow) appendDebug(info string) {
	pc, fp, line, _ := runtime.Caller(f.getCaller())
	name := runtime.FuncForPC(pc).Name()
	stack := fmt.Sprintf("%v:%v %v", path.Base(fp), line, path.Base(name))
	f.debug = append(f.debug, debugInfo{stack, info})
}

func (f *Flow) SetOnClose(exit func()) *Flow {
	f.onClose = []func(){exit}
	return f
}

func (f *Flow) AddOnClose(exit func()) *Flow {
	f.onClose = append(f.onClose, exit)
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

func (f *Flow) ForkTo(ref **Flow, exit func()) {
	*ref = f.Fork(0).AddOnClose(exit)
}

func (f *Flow) Fork(n int) *Flow {
	f2 := NewEx(n)
	f2.Parent = f
	// TODO(chzyer): test it !
	f2.errChan = f.errChan
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
	f.appendDebug("close")
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
	f.appendDebug("stop")

	close(f.stopChan)
	for _, cf := range f.Children {
		cf.Stop()
	}
	if len(f.onClose) > 0 {
		go func() {
			for _, f := range f.onClose {
				f()
			}
		}()
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
	f.appendDebug(fmt.Sprintf("add: %v, ref: %v", n, *f.ref))
	f.wg.Add(n)
}

func (f *Flow) getCaller() int {
	for i := 0; ; i++ {
		pc, _, _, ok := runtime.Caller(i)
		if !ok {
			break
		}
		f := runtime.FuncForPC(pc).Name()
		if !strings.HasPrefix(f, pkgPath) {
			return i - 1
		}
	}
	return 0
}

func (f *Flow) Done() {
	f.wg.Done()
	if atomic.AddInt32(f.ref, -1) == 0 {
		f.Stop()
	}
}

func (f *Flow) DoneAndClose() {
	f.Done()
	f.appendDebug(fmt.Sprintf("done and close, ref: %v", *f.ref))
	f.close()
}

func (f *Flow) wait() {
	f.appendDebug("wait")

	done := make(chan struct{})
	if DefaultDebug && atomic.CompareAndSwapInt32(&f.printed, 0, 1) {
		go func() {
			select {
			case <-done:
				return
			case <-time.After(1000 * time.Millisecond):
				f.printDebug()
			}
		}()
	}
	<-f.stopChan
	f.wg.Wait()
	close(done)

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
		f.appendDebug("got closed")
	case <-signalChan:
		f.appendDebug("got signal")
		f.Stop()
	case err = <-f.errChan:
		f.appendDebug(fmt.Sprintf("got error: %v", err))

		if err != nil {
			f.Stop()
		}
	}

	go func() {
		<-signalChan
		// force close
		println("force close")
		os.Exit(1)
	}()

	f.wait()
	return err
}
