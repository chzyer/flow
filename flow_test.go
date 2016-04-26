package flow

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestStopWaitFork(t *testing.T) {
	f := New()

	done := make(chan struct{})

	{ // worker
		go func() {
			f.Add(1)
			defer f.DoneAndClose()
			<-done
		}()
	}

	go func() {
		for _ = range time.Tick(time.Second) {
			println(string(f.GetDebug()))
			break
		}
	}()

	go func() {
		f.Stop()
		{
			f2 := f.Fork(0)
			done2 := make(chan struct{})
			go func() {
				f2.Add(1)
				<-done2
				f2.Done()
			}()
			close(done2)
			f2.Wait()
		}
		time.Sleep(100 * time.Millisecond)
		close(done)
	}()
	f.Wait()
}

func TestFlow(t *testing.T) {
	{
		f := NewEx(1)
		done := 0
		go func() {
			defer f.Done()
			for i := 0; i < 3; i++ {
				done++
			}
		}()

		f.Wait()
		if done != 3 {
			t.Fatal(done)
		}
	}

	{
		f := NewEx(2)
		var done int64
		go func() {
			defer f.Done()
			atomic.AddInt64(&done, 1)
		}()
		go func() {
			defer f.Done()
			atomic.AddInt64(&done, 1)
		}()
		f.Wait()
		if done != 2 {
			t.Fatal(done)
		}
	}
}
