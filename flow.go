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
	DefaultDebug = true
)

type debugInfo struct {
	Time     string
	FileInfo string
	Stack    string
	Info     string
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
	if n > 0 {
		f.Add(n)
	}
	return f
}

func New() *Flow {
	return NewEx(0)
}

func (f *Flow) WaitNotify(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-f.IsClose():
		return false
	}
}

func (f *Flow) MarkExit() bool {
	return atomic.CompareAndSwapInt32(&f.exited, 0, 1)
}

func (f *Flow) IsExit() bool {
	return atomic.LoadInt32(&f.exited) == 1
}

func (f *Flow) GetDebug() []byte {
	buf := bytes.NewBuffer(nil)
	var stackML, fileML, timeML int
	for _, d := range f.debug {
		if stackML < len(d.Stack) {
			stackML = len(d.Stack)
		}
		if fileML < len(d.FileInfo) {
			fileML = len(d.FileInfo)
		}
		if timeML < len(d.Time) {
			timeML = len(d.Time)
		}
	}
	fill := func(a string, n int) string {
		return a + strings.Repeat(" ", n-len(a))
	}
	buf.WriteString("\n")
	for _, d := range f.debug {
		buf.WriteString(fmt.Sprintf("%v %v %v - %v\n",
			fill(d.Time, timeML),
			fill(d.FileInfo, fileML),
			fill(d.Stack, stackML), d.Info,
		))
	}
	return buf.Bytes()
}

func (f *Flow) printDebug() {
	println(string(f.GetDebug()))
}

func (f *Flow) appendDebug(info string) {
	pc, fp, line, _ := runtime.Caller(f.getCaller())
	name := runtime.FuncForPC(pc).Name()
	f.debug = append(f.debug, debugInfo{
		Time:     time.Now().Format("02 15:04:05"),
		FileInfo: fmt.Sprintf("%v:%v", path.Base(fp), line),
		Stack:    path.Base(name),
		Info:     info,
	})
}

func (f *Flow) SetOnClose(exit func()) *Flow {
	if f.IsClosed() {
		f.appendDebug("set close after closed")
		exit()
		return f
	}

	f.onClose = []func(){exit}
	return f
}

func (f *Flow) AddOnClose(exit func()) *Flow {
	if f.IsClosed() {
		f.appendDebug("add close after closed")
		exit()
		return f
	}

	f.onClose = append(f.onClose, exit)
	return f
}

const (
	F_CLOSED  = true
	F_TIMEOUT = false
)

func (f *Flow) Tick(t *time.Ticker) bool {
	select {
	case <-t.C:
		return F_TIMEOUT
	case <-f.IsClose():
		return F_CLOSED
	}
}

func (f *Flow) CloseOrWait(duration time.Duration) bool {
	select {
	case <-time.After(duration):
		return F_TIMEOUT
	case <-f.IsClose():
		return F_CLOSED
	}
}

func (f *Flow) Errorf(layout string, obj ...interface{}) {
	f.Error(fmt.Errorf(layout, obj...))
}

func (f *Flow) Error(err error) {
	f.errChan <- err
}

func (f *Flow) ForkTo(ref **Flow, exit func()) {
	child := f.Fork(0)
	*ref = child
	child.AddOnClose(exit)
}

func (f *Flow) Fork(n int) *Flow {
	f2 := NewEx(n)
	f2.Parent = f
	// TODO(chzyer): test it !
	f2.errChan = f.errChan
	f.Children = append(f.Children, f2)
	f.Add(1) // for f2

	if f.IsClosed() {
		f.appendDebug("fork when closed")
		// stop-wait
		// ->fork
		// done
		f2.Stop()
	}

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
	ref := atomic.AddInt32(f.ref, int32(n))
	f.appendDebug(fmt.Sprintf("add: %v, ref: %v", n, ref))
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

func (f *Flow) getRef() int32 {
	return atomic.LoadInt32(f.ref)
}

func (f *Flow) Done() {
	f.wg.Done()
	ref := atomic.AddInt32(f.ref, -1)
	f.appendDebug(fmt.Sprintf("done, ref: %v", ref))
	if ref == 0 {
		f.Stop()
	}
}

func (f *Flow) DoneAndClose() {
	f.wg.Done()
	ref := atomic.AddInt32(f.ref, -1)
	f.appendDebug(fmt.Sprintf("done and close, ref: %v", ref))
	f.Stop()
}

func (f *Flow) wait() {
	f.appendDebug("wait")

	done := make(chan struct{})
	printed := int32(0)
	if DefaultDebug && atomic.CompareAndSwapInt32(&f.printed, 0, 1) {
		go func() {
			select {
			case <-done:
				return
			case <-time.After(1000 * time.Millisecond):
				f.printDebug()
				atomic.StoreInt32(&printed, 1)
			}
		}()
	}
	<-f.stopChan
	f.wg.Wait()
	close(done)
	if atomic.LoadInt32(&printed) == 1 {
		println(fmt.Sprint(&f) + " - exit")
	}

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
