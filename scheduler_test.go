package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestScheduler(t *testing.T) {
	s := New()
	s.Schedule(1, func(context.Context) { t.Log("Hello 1") })
	s.Schedule(3, func(context.Context) { t.Log("Hello 2") })
	s.Schedule(2, func(context.Context) { t.Log("Hello 3") })
	s.Schedule(4, func(context.Context) { t.Log("Hello 4") })
	s.Schedule(3, func(context.Context) { t.Log("Hello 5") })
	s.Schedule(func(context.Context) { t.Log("Hello 6") })
	_, err := s.Schedule(1, "*/5 * * * * *", func(context.Context) { t.Log("Hello 7") })
	if err != nil {
		t.Log(err)
	}

	time.Sleep(time.Second * 30)
}
