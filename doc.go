/*
Package scheduler implements a scheduler spec parser and job runner, and support start a delay job that
execute only one time, and won't execute every job in a single goroutine, instead, user can specify a number
to inform scheduler that how many goroutines you wants it to created, and each goroutine will not exit after
one job is done, and it will wait for next job, and will exit if no new job available in one minute.

Installation
To download the specific tagged release, run:
	go get github.com/derekhjray/scheduler
Import it in your program as:
	import "github.com/derekhjray/schuduler"
It requires Go 1.11 or later due to usage of Go Modules.
Usage
Callers may register Funcs to be invoked on a given schedule.  Cron will run
them in their own goroutines.
	s := scheduler.New(scheduler.WithGoroutines(4)) // Create 4 goroutines at most
	s.Schedule("30 * * * *", func(context.Context) { fmt.Println("Every hour on the half hour") })
	s.Schedule("30 3-6,20-23 * * *", func(context.Context) { fmt.Println(".. in the range 3-6am, 8-11pm") })
	s.Schedule("CRON_TZ=Asia/Tokyo 30 04 * * *", func(context.Context) { fmt.Println("Runs at 04:30 Tokyo time every day") })
	s.Schedule("@hourly",      func(context.Context) { fmt.Println("Every hour, starting an hour from now") })
	s.Schedule("@every 1h30m", func(context.Context) { fmt.Println("Every hour thirty, starting an hour thirty from now") })
	s.Schedule(10, func(context.Context) { fmt.Println("Execute after 10 seconds and quit") })
	s.Schedule(10, "30 * * * *", func(context.Context) { fmt.Println("Execute after 10 seconds and Every hour on the half hour") })

	...
	s.Unschedule(job_ids)  // Remove all jobs in job_ids, and call context.CancelFunc to cancel runing jobs
	s.Reschedule(id, spec) // Reschedule job with a new spec

Scheduler Expression Format
A scheduler expression represents a set of times, using 5 space-separated fields.
	Field name   | Mandatory? | Allowed values  | Allowed special characters
	----------   | ---------- | --------------  | --------------------------
	Minutes      | Yes        | 0-59            | * / , -
	Hours        | Yes        | 0-23            | * / , -
	Day of month | Yes        | 1-31            | * / , - ?
	Month        | Yes        | 1-12 or JAN-DEC | * / , -
	Day of week  | Yes        | 0-6 or SUN-SAT  | * / , - ?
Month and Day-of-week field values are case insensitive.  "SUN", "Sun", and
"sun" are equally accepted.
The specific interpretation of the format is based on the Cron Wikipedia page:
https://en.wikipedia.org/wiki/Cron
Alternative Formats
Alternative Cron expression formats support other fields like seconds. You can
implement that by creating a custom Parser as follows.
	scheduler.New(
		scheduler.WithParser(
			scheduler.NewParser(
				scheduler.SecondOptional | scheduler.Minute | scheduler.Hour | scheduler.Dom | scheduler.Month | scheduler.Dow | scheduler.Descriptor)))

That emulates Quartz, the most popular alternative Cron schedule format:
http://www.quartz-scheduler.org/documentation/quartz-2.x/tutorials/crontrigger.html
Special Characters
Asterisk ( * )
The asterisk indicates that the cron expression will match for all values of the
field; e.g., using an asterisk in the 5th field (month) would indicate every
month.
Slash ( / )
Slashes are used to describe increments of ranges. For example 3-59/15 in the
1st field (minutes) would indicate the 3rd minute of the hour and every 15
minutes thereafter. The form "*\/..." is equivalent to the form "first-last/...",
that is, an increment over the largest possible range of the field.  The form
"N/..." is accepted as meaning "N-MAX/...", that is, starting at N, use the
increment until the end of that specific range.  It does not wrap around.
Comma ( , )
Commas are used to separate items of a list. For example, using "MON,WED,FRI" in
the 5th field (day of week) would mean Mondays, Wednesdays and Fridays.
Hyphen ( - )
Hyphens are used to define ranges. For example, 9-17 would indicate every
hour between 9am and 5pm inclusive.
Question mark ( ? )
Question mark may be used instead of '*' for leaving either day-of-month or
day-of-week blank.
Predefined schedules
You may use one of several pre-defined schedules in place of a cron expression.
	Entry                  | Description                                | Equivalent To
	-----                  | -----------                                | -------------
	@yearly (or @annually) | Run once a year, midnight, Jan. 1st        | 0 0 1 1 *
	@monthly               | Run once a month, midnight, first of month | 0 0 1 * *
	@weekly                | Run once a week, midnight between Sat/Sun  | 0 0 * * 0
	@daily (or @midnight)  | Run once a day, midnight                   | 0 0 * * *
	@hourly                | Run once an hour, beginning of hour        | 0 * * * *
Intervals
You may also schedule a job to execute at fixed intervals, starting at the time it's added
or scheduler is run. This is supported by formatting the cron spec like this:
    @every <duration>
where "duration" is a string accepted by time.ParseDuration
(http://golang.org/pkg/time/#ParseDuration).
For example, "@every 1h30m10s" would indicate a schedule that activates after
1 hour, 30 minutes, 10 seconds, and then every interval after that.
Note: The interval does not take the job runtime into account.  For example,
if a job takes 3 minutes to run, and it is scheduled to run every 5 minutes,
it will have only 2 minutes of idle time between each run.
Time zones
By default, all interpretation and scheduling is done in the machine's local
time zone (time.Local). You can specify a different time zone on construction:
      scheduler.New(
          scheduler.WithLocation(time.UTC))
Individual scheduler schedules may also override the time zone they are to be
interpreted in by providing an additional space-separated field at the beginning
of the scheduler spec, of the form "CRON_TZ=Asia/Tokyo".
For example:
	# Runs at 6am in time.Local
	scheduler.New().Schedule("0 6 * * ?", ...)
	# Runs at 6am in America/New_York
	nyc, _ := time.LoadLocation("America/New_York")
	s := scheduler.New(scheduler.WithLocation(nyc))
	s.Schedule("0 6 * * ?", ...)
	# Runs at 6am in Asia/Tokyo
	scheduler.New().Schedule("CRON_TZ=Asia/Tokyo 0 6 * * ?", ...)
	# Runs at 6am in Asia/Tokyo
	s := scheduler.New(scheduler.WithLocation(nyc))
	s.Schedule("CRON_TZ=Asia/Tokyo 0 6 * * ?", ...)
The prefix "TZ=(TIME ZONE)" is also supported for legacy compatibility.
Be aware that jobs scheduled during daylight-savings leap-ahead transitions will
not be run!
Thread safety
Since the Scheduler service runs concurrently with the calling code, some amount of
care must be taken to ensure proper synchronization.
All scheduler methods are designed to be correctly synchronized as long as the caller
ensures that invocations have a clear happens-before ordering between them.
Implementation
Scheduler entries are stored in an array, sorted by their next activation time.  Scheduler
sleeps until the next job is due to be run.
Upon waking:
 - it runs each entry that is active on that second
 - it calculates the next run times for the jobs that were run
 - it re-sorts the array of entries by next activation time.
 - it goes to sleep until the soonest job.
*/
package scheduler
