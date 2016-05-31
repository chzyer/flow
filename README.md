# flow
Help you to control the flow of goroutines



## Feature



##### Wait on all goroutines exited, just like `sync.WaitGroup`

```go
func main() {
	f := flow.New()
  	go func() {
  		f.Add(1)
      	defer f.Done()
      	println("exit")
	}()
  	f.Wait() // wait will capture signals
}
// output: 
// exit
```



##### Notify goroutines to exit

```go
func main() {
	f := flow.New()
	go func() {
		f.Add(1)
		defer f.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

	loop:
		for {
			select {
			case <-ticker.C:
				println("tick")
			case <-f.IsClose():
              	println("break")
				break loop
			}
		}
	}()

	go func() {
		time.Sleep(2 * time.Second)
		f.Close()
	}()

	f.Wait()
  	println("exited")
}
// output: 
// tick
// tick
// break
// exited
```

If we kill this process by `Ctrl+C` in 1s, we will got output as follows:

```go
// output:
// tick
// break
// exited
```



##### Multiple goroutines can all run or all die

```go
func main() {
	f := flow.New()

	ch := make(chan string)
	// read
	go func() {
		f.Add(1)
		defer f.DoneAndClose() // Done and also close this flow
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		exitTime := time.Now().Add(3 * time.Second)

	loop:
		for {
			select {
			case now := <-ticker.C:
				if now.After(exitTime) {
					break loop
				}
				ch <- now.String()
			case <-f.IsClose():
				break loop
			}
		}

		println("readloop exit")
	}()

	go func() {
		f.Add(1)
		defer f.DoneAndClose() // Done and also close this flow

	loop:
		for {
			select {
			case text := <-ch:
				fmt.Fprintln(os.Stdout, text)
			case <-f.IsClose():
				break loop
			}
		}

		println("writeloop exit")
	}()

	f.Wait()
	println("all exit")
}

// output:
// 2016-05-31 18:10:18.525209975 +0800 CST
// 2016-05-31 18:10:19.525009926 +0800 CST
// readloop exit
// writeloop exit
```



##### Indicate leaking goroutine

```Go
func goroutine1(f *flow.Flow) {
	f.Add(1)
	defer f.DoneAndClose()
	for _ = range time.Tick(time.Second) {

	}
	println("goroutine 1 exit")
}

func goroutine2(f *flow.Flow) {
	f.Add(1)
	defer f.DoneAndClose()
loop:
	for {
		select {
		case <-f.IsClose():
			break loop
		}
	}
	println("goroutine 2 exit")
}

func main() {
	f := flow.New()
	go goroutine1(f)
	go goroutine2(f)
	f.Wait()
}
// output:
// (press Ctrl+C)
// goroutine 2 exit

// 31 18:18:59 flow-wait.go:124 main.main       - init
// 31 18:18:59 flow-wait.go:103 main.goroutine1 - add: 1, ref: 1
// 31 18:18:59 flow-wait.go:111 main.goroutine2 - add: 1, ref: 2
// 31 18:19:00 flow-wait.go:127 main.main       - got signal
// 31 18:19:00 flow-wait.go:127 main.main       - stop
// 31 18:19:00 flow-wait.go:127 main.main       - wait
// 31 18:19:00 flow-wait.go:121 main.goroutine2 - done and close, ref: 1

// (press Ctrl+C again)
// force close
```

If the `flow.Wait()` waiting too long, it will print some debug info to let you indicate which goroutine is leaked. For example above, we know that `main.goroutine1` is leaked. 



##### Raise error out to main goroutine from sub-goroutine

```Go
func main() {
	f := flow.New()

	go func() {
		err := http.ListenAndServe(":80", nil)
		if err != nil {
			f.Error(err)
			return
		}
	}()

	if err := f.Wait(); err != nil {
 	    println("error:", err.Error())
		os.Exit(1)
	}
}

// output:
// error: listen tcp :80: bind: permission denied
// exit status 1
```

