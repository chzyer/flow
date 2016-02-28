package flow

import (
	"sync/atomic"
	"testing"
)

func TestFlow(t *testing.T) {
	{
		f := New(1)
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
		f := New(2)
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
