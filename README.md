# flow
Help you to control the flow of goroutine

```{go}
f := flow.New(1)
var done int32
go func() {
	defer f.Done()
	for i := 0; i < 3; i++ {
		atomic.AddInt32(&done, 1)
	}
}()

f.Wait()
println(done)
// 3
```

```{go}
f := flow.New(0)

go func() {
	err := http.ListenAndServe(":-1", nil)
	if err != nil {
		f.Error(err)
		return
	}
}()

var done int32

go func() {
	f.Add(1)
	defer f.Done()

	time.Sleep(time.Second)
	atomic.StoreInt32(&done, 1)
}()

err := f.Wait()
if err != nil {
	println(err.Error())
	// error
}
println(done)
// 0
```

```{go}
f := flow.New(0)
go func() {
	f.Add(1)
	defer f.Done()
loop:
	for {
		select {
		case <-f.IsClose():
			time.Sleep(time.Second)
			break loop
		}
	}
	println("exited!")
}()
go func() {
	time.Sleep(time.Second)
	f.Stop()
}()
f.Wait()
// about 2 second
// exited!
```


```{go}
f := flow.New(0)
go func() {
	f.Add(1)
	defer f.Done()
loop:
	for {
		select {
		case <-f.IsClose():
			time.Sleep(time.Second)
			break loop
		}
	}
	println("exited!")
}()

f.Wait()
// Ctrl+C -> wait about 1 second
// exited!
```
