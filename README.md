# Scheduler
This a simple and efficient job scheduler based on https://github.com/robfig/cron, support most of cron features and add some new features

---
# Features
- Schedule a periodically job with spec
- Schedule a delayed periodically job with delay duration and spec
- Schedule a delayed job execute only once
- Unschedule a job, and cancel running task with context.CancelFunc
- Reschedule a job with new spec
- Specify goroutine count that used to executing jobs
- Specify a customed timezone

# Installation
```bash
go get github.com/derekhjray/scheduler
```

# Usage
```go
import (
    "log"
    "github.com/derekhjray/scheduler"
)

func main() {
    // use 8 goroutines at most
    opt := scheduler.WithGoroutines(8)
    s := scheduler.New(opt)

    s.Schedule(10, func(ctx context.Context) { log.Println("Execute a job after 10 seconds") })
    s.Schedule("@hourly", func(ctx context.Context) { log.Println("Execute a job every hour") })
    s.Schedule("*/15 * * * * *", func(ctx context.Context) { log.Println("Execute a job every 15 seconds") })
    s.Schedule(10, "*/15 * * * * *", func(ctx context.Context) { log.Println("Execute a job after 10 seconds for the first time, and every 15 seconds after") })

    ...
    s.Unschedule(ids...)   // Unschedule jobs, and cancel running job with context
    s.Reschedule(id, spec) // Reschedule a job with a new spec
}
```
