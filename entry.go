package scheduler

import (
	"context"
	"time"
)

type Runnable func(context.Context)

func (r Runnable) Run(ctx context.Context) {
	r(ctx)
}

type Job interface {
	Run(context.Context)
}

type Schedule interface {
	Next(time.Time) time.Time
}

type Entry struct {
	ID       int64
	Prev     time.Time
	Next     time.Time
	Job      Job
	Once     bool
	Schedule Schedule
	Cancel   context.CancelFunc
	Context  context.Context
	state    int32
}

const (
	sleeping = iota
	executing
	cancelled
	removing
)
