// Package scheduler implements a cron and async task executing mechanism, which support cron task,
// async task (delay execute or execute immediately)
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync/atomic"
	"time"

	"log"
)

type Scheduler interface {
	Schedule(...interface{}) (int64, error)
	Reschedule(int64, string) error
	Unschedule(...int64) error
}

type Option func(Scheduler)

func WithLocation(loc *time.Location) Option {
	return func(s Scheduler) {
		if ss, ok := s.(*scheduler); ok {
			ss.location = loc
		}
	}
}

func WithGoroutines(maxGoroutines int) Option {
	return func(s Scheduler) {
		if ss, ok := s.(*scheduler); ok {
			ss.maxGoroutines = int32(maxGoroutines)
		}
	}
}

func WithParser(parser Parser) Option {
	return func(s Scheduler) {
		if ss, ok := s.(*scheduler); ok {
			ss.parser = parser
		}
	}
}

type scheduler struct {
	sequence      int64
	entries       []*Entry
	indexes       map[int64]int
	parser        Parser
	running       int32
	location      *time.Location
	goroutines    int32
	maxGoroutines int32
	idles         int32
	tasks         chan *Entry
	events        chan *event
}

func New(options ...Option) Scheduler {
	s := &scheduler{
		entries:  make([]*Entry, 0, 64),
		indexes:  make(map[int64]int),
		tasks:    make(chan *Entry),
		events:   make(chan *event),
		parser:   NewParser(Second | Minute | Hour | Dom | Month | Dow | Descriptor),
		location: time.Local,
	}

	for _, option := range options {
		option(s)
	}

	if s.maxGoroutines == 0 {
		s.maxGoroutines = int32(runtime.NumCPU())
		log.Printf("goroutines: %d\n", s.maxGoroutines)
	}

	return s
}

// Schedule add job to schedule list, this function accepts three arguments at most, the last argument MUST be Job/Runnable/func(context.Context),
// if a cron spec (string) specified in first argument, this job will be scheduled periodically util job removing from schedule list. And if a
// integer(seconds)/time.Duration argument specified in the first argument, this job will be scheduled after duration specified, if the second
// argument is cron spec, the job will be scheduled periodically after first execution, otherwise, job will be removed after first execution.
// If only one argument specified, this job will be scheduled immediately
func (s *scheduler) Schedule(args ...interface{}) (int64, error) {
	var (
		spec     string
		job      Job
		size     int
		duration time.Duration = -1
		ok       bool
		err      error
	)

	size = len(args)

	switch j := args[size-1].(type) {
	case Job:
		job = j
	case func(context.Context):
		job = Runnable(j)
	default:
		return 0, errors.New("the last argument MUST be Job/Runnable/func(context.Context)")
	}

	if size == 0 {
		return 0, errors.New("no argument specified, at least a argument with type Job/Runnable/func(context.Context) required")
	} else if size == 1 {
		duration = 0
	} else if size <= 3 {
		switch v := args[0].(type) {
		case string:
			spec = v
			if size == 3 {
				return 0, errors.New("found too many arguments specified for first argument is cron spec")
			}
		case int8:
			duration = time.Second * time.Duration(v)
		case uint8:
			duration = time.Second * time.Duration(v)
		case int16:
			duration = time.Second * time.Duration(v)
		case uint16:
			duration = time.Second * time.Duration(v)
		case int32:
			duration = time.Second * time.Duration(v)
		case uint32:
			duration = time.Second * time.Duration(v)
		case int64:
			duration = time.Second * time.Duration(v)
		case uint64:
			duration = time.Second * time.Duration(v)
		case int:
			duration = time.Second * time.Duration(v)
		case uint:
			duration = time.Second * time.Duration(v)
		case time.Duration:
			duration = v
		default:
			return 0, fmt.Errorf("found illegal type for first argument, which string/integer/time.Duration required")
		}

		if size == 3 {
			if spec, ok = args[1].(string); !ok {
				return 0, errors.New("found illegal second argument for the first argument is a delay duration, cron spec required")
			}
		}
	} else {
		return 0, errors.New("too many arguments specified, scheduler accepts three arguments at most")
	}

	for _, arg := range args {
		switch v := arg.(type) {
		case Job:
			job = v
		case func(context.Context):
			job = Runnable(v)
		case string:
			spec = v
		case int8:
			duration = time.Second * time.Duration(v)
		case uint8:
			duration = time.Second * time.Duration(v)
		case int16:
			duration = time.Second * time.Duration(v)
		case uint16:
			duration = time.Second * time.Duration(v)
		case int32:
			duration = time.Second * time.Duration(v)
		case uint32:
			duration = time.Second * time.Duration(v)
		case int64:
			duration = time.Second * time.Duration(v)
		case uint64:
			duration = time.Second * time.Duration(v)
		case int:
			duration = time.Second * time.Duration(v)
		case uint:
			duration = time.Second * time.Duration(v)
		case time.Duration:
			duration = v
		default:
			return 0, fmt.Errorf("found illegal argument '%v'", v)
		}
	}

	if job == nil {
		return 0, errors.New("scheduler found missing argument, one argument typed Job/Runnable/func(context.Context) required")
	}

	entry := &Entry{
		ID:   atomic.AddInt64(&s.sequence, 1),
		Job:  job,
		Once: len(spec) == 0,
		Prev: time.Unix(0, 0),
		Next: time.Now().In(s.location).Add(duration),
	}

	tm := time.Now().In(s.location)
	if len(spec) > 0 {
		if entry.Schedule, err = s.parser.Parse(spec); err != nil {
			return 0, err
		}

		if duration == -1 {
			// no delay for first execution
			entry.Next = entry.Schedule.Next(tm)
		}
	}

	if atomic.LoadInt32(&s.running) == 0 {
		state := make(chan struct{})

		go s.run(state)
		<-state
		close(state)
	}

	s.events <- &event{action: schedule, entry: entry}

	return entry.ID, nil
}

var ErrSchedulerClosed = errors.New("scheduler: closed")

func (s *scheduler) Reschedule(id int64, spec string) (err error) {
	if atomic.LoadInt32(&s.running) == 0 {
		return ErrSchedulerClosed
	}

	entry := &Entry{ID: id}
	if entry.Schedule, err = s.parser.Parse(spec); err != nil {
		return err
	}

	s.events <- &event{action: reschedule, entry: entry}

	return nil
}

func (s *scheduler) Unschedule(ids ...int64) error {
	if atomic.LoadInt32(&s.running) == 0 {
		return ErrSchedulerClosed
	}

	s.events <- &event{action: unschedule, ids: ids}

	return nil
}

func (s *scheduler) run(state chan struct{}) {
	var (
		timer   *time.Timer
		tm      time.Time
		index   int
		canStop bool
	)

	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&s.running, 0)

	s.events = make(chan *event)
	s.tasks = make(chan *Entry)

	defer func() {
		close(s.events)
		close(s.tasks)
	}()

	state <- struct{}{}

	for {
		canStop = false
		timer = nil

		if len(s.entries) > 0 {
			sort.Sort(s)

			// Remove all nil entries
			for index = len(s.entries) - 1; index >= 0; index-- {
				if s.entries[index] != nil {
					index++
					break
				}
			}
			s.entries = s.entries[:index]

			tm = time.Now().In(s.location)

			for _, e := range s.entries {
				if atomic.LoadInt32(&e.state) != sleeping || e.Next.IsZero() {
					continue
				}

				d := e.Next.Sub(tm)
				if d < 0 {
					d = time.Microsecond
				}

				timer = time.NewTimer(d)
				break
			}

			if timer == nil {
				// If there are no active entries yet, just sleep for task state change and handle new task requests
				timer = time.NewTimer(100000 * time.Hour)
			}
		} else {
			// If there are no entries yet, sleep 1 minute for new task, otherwise, stop scheduler loop
			timer = time.NewTimer(time.Minute)
			canStop = true
		}

		select {
		case tm = <-timer.C:
			if len(s.entries) == 0 && canStop {
				log.Println("Exiting scheduler event loop")
				return
			}

			tm = tm.In(s.location)

			// Run every entry whose next time was less than now
			for _, e := range s.entries {
				if e.Next.After(tm) || e.Next.IsZero() {
					break
				}

				if atomic.LoadInt32(&e.state) != sleeping {
					continue
				}

				if !s.execute(e) {
					// all worker is busy
					break
				}
			}
		case evt := <-s.events:
			timer.Stop()
			tm = time.Now().In(s.location)
			switch evt.action {
			case schedule:
				s.entries = append(s.entries, evt.entry)
				s.indexes[evt.entry.ID] = len(s.entries) - 1
			case reschedule:
				index := s.indexes[evt.entry.ID]
				if index >= 0 && index < len(s.entries) && s.entries[index] != nil {
					s.entries[index].Schedule = evt.entry.Schedule
				}
			case unschedule:
				s.removeEntry(evt.ids...)
			case remove:
				s.removeEntry(evt.entry.ID)
			case update:
				evt.entry.Prev = evt.entry.Next
				evt.entry.Next = evt.entry.Schedule.Next(tm)
			}
		}
	}
}

type JobID string

const (
	SCHEDULE_JOB_ID JobID = "schedule_job_id"
)

func (s *scheduler) execute(entry *Entry) (scheduled bool) {
	if atomic.LoadInt32(&s.goroutines) < s.maxGoroutines && atomic.LoadInt32(&s.idles) <= 0 {
		state := make(chan struct{})

		go func() {
			var action int
			atomic.AddInt32(&s.goroutines, 1)
			defer atomic.AddInt32(&s.goroutines, -1)

			atomic.AddInt32(&s.idles, 1)

			state <- struct{}{}
			log.Println("Start a new schedule worker")
			for {
				select {
				case <-time.After(time.Minute):
					atomic.AddInt32(&s.idles, -1)
					log.Println("Exiting scheduler worker goroutine")
					return
				case e := <-s.tasks:
					if e == nil {
						// closed
						return
					}

					e.Context, e.Cancel = context.WithCancel(context.WithValue(context.Background(), SCHEDULE_JOB_ID, e.ID))
					e.Job.Run(e.Context)
					e.Cancel()

					if e.Once || atomic.LoadInt32(&e.state) == cancelled {
						atomic.StoreInt32(&e.state, removing)
						action = remove
					} else {
						atomic.StoreInt32(&e.state, sleeping)
						action = update
					}

					s.events <- &event{action: action, entry: e}

					atomic.AddInt32(&s.idles, 1)
				}
			}
		}()
		<-state
		close(state)
	}

	if atomic.AddInt32(&s.idles, -1) >= 0 {
		atomic.StoreInt32(&entry.state, executing)
		s.tasks <- entry
		return true
	}

	atomic.AddInt32(&s.idles, 1)
	return false
}

func (s *scheduler) removeEntry(ids ...int64) {
	var (
		e     *Entry
		size  int
		index int
	)

	if len(ids) == 0 {
		ids = make([]int64, len(s.entries))
		index = 0
		for key := range s.indexes {
			ids[index] = key
			index++
		}
	}

	size = len(s.entries)
	for _, id := range ids {
		index = s.indexes[id]
		if index >= 0 && index < size && s.entries[index] != nil {
			e = s.entries[index]
			if e.Cancel != nil {
				e.Cancel()
			}

			if atomic.LoadInt32(&e.state) != executing {
				delete(s.indexes, id)
				s.entries[index] = nil
				continue
			}
			atomic.StoreInt32(&e.state, cancelled)
		}
	}
}

func (s *scheduler) Len() int {
	return len(s.entries)
}

func (s *scheduler) Less(i, j int) bool {
	if s.entries[i] == nil {
		return false
	}

	if s.entries[j] == nil {
		return true
	}

	// Two zero times should return false.
	// Otherwise, zero is "greater" than any other time.
	// (To sort it at the end of the list.)
	if s.entries[i].Next.IsZero() {
		return false
	}

	if s.entries[j].Next.IsZero() {
		return true
	}

	return s.entries[i].Next.Before(s.entries[j].Next)
}

func (s *scheduler) Swap(i, j int) {
	if s.entries[i] != nil {
		s.indexes[s.entries[i].ID] = j
	}

	if s.entries[j] != nil {
		s.indexes[s.entries[j].ID] = i
	}

	s.entries[i], s.entries[j] = s.entries[j], s.entries[i]
}

const (
	schedule = iota
	reschedule
	unschedule
	remove
	update
)

type event struct {
	action int
	entry  *Entry
	ids    []int64
}
