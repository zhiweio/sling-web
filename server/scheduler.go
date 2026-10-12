// scheduler.go runs job schedules with robfig/cron. Each unique timezone
// gets its own cron instance; jobs register by ID so edits apply live.
// On boot, orphaned running rows are marked interrupted and missed
// fire times are skipped (misfire policy: skip + log).
package server

import (
	"sync"
	"time"

	"github.com/flarco/g"
	"github.com/robfig/cron/v3"
)

// Scheduler dispatches scheduled job runs to the executor.
type Scheduler struct {
	db  *DB
	exe *Executor

	mu    sync.Mutex
	crons map[string]*cron.Cron // tz name -> cron instance
	ids   map[string]cron.EntryID // job id -> entry (within its tz cron)
	tzs   map[string]string       // job id -> tz name
}

func NewScheduler(db *DB, exe *Executor) *Scheduler {
	return &Scheduler{
		db:    db,
		exe:   exe,
		crons: map[string]*cron.Cron{},
		ids:   map[string]cron.EntryID{},
		tzs:   map[string]string{},
	}
}

func (s *Scheduler) cronFor(tzName string) (*cron.Cron, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.crons[tzName]; ok {
		return c, nil
	}
	loc := time.Local
	if tzName != "" {
		var err error
		loc, err = time.LoadLocation(tzName)
		if err != nil {
			return nil, g.Error(err, "unknown timezone %s", tzName)
		}
	}
	c := cron.New(cron.WithLocation(loc), cron.WithChain(cron.Recover(cron.DefaultLogger)))
	c.Start()
	s.crons[timeKey(loc, tzName)] = c
	return c, nil
}

func timeKey(loc *time.Location, tzName string) string {
	if tzName != "" {
		return tzName
	}
	return loc.String()
}

// Boot recovers state after a restart: mark orphaned runs interrupted,
// re-register enabled schedules, skip missed fire times.
func (s *Scheduler) Boot() error {
	// 1. orphan recovery
	res := s.db.Model(&Run{}).
		Where("status IN ?", []string{"queued", "running", "created"}).
		Updates(map[string]any{"status": "interrupted", "error": "server restarted while running"})
	if res.Error != nil {
		return g.Error(res.Error, "could not recover orphaned runs")
	}
	if res.RowsAffected > 0 {
		g.Warn("marked %d orphaned run(s) as interrupted after restart", res.RowsAffected)
	}

	// 2. register schedules
	var jobs []Job
	if err := s.db.Where("schedule_enabled = ? AND schedule_cron != ''", true).Find(&jobs).Error; err != nil {
		return g.Error(err, "could not load scheduled jobs")
	}
	for i := range jobs {
		job := jobs[i]
		if err := s.AddJob(&job); err != nil {
			g.Warn("could not schedule %s: %s", job.Name, err)
			continue
		}
		// misfire policy: missed window -> skip, log once
		if job.NextRunAt != nil && job.NextRunAt.Before(time.Now()) {
			s.db.Model(&Job{}).Where("id = ?", job.ID).
				Update("next_run_at", s.NextRun(&job, 1))
			s.db.Audit("misfire", "job", job.ID, "skipped missed schedule after restart")
		}
	}
	return nil
}

// AddJob (re)registers a job's schedule.
func (s *Scheduler) AddJob(job *Job) error {
	if !job.ScheduleEnabled || job.ScheduleCron == "" {
		s.RemoveJob(job.ID)
		return nil
	}
	c, err := s.cronFor(job.ScheduleTZ)
	if err != nil {
		return err
	}

	s.RemoveJob(job.ID)

	entryID, err := c.AddFunc(job.ScheduleCron, func() {
		// reload the job so edits apply without re-registration
		fresh, err := s.db.GetJob(job.ID)
		if err != nil {
			return
		}
		if !fresh.ScheduleEnabled || fresh.AutoPaused {
			return
		}
		next := s.NextRun(fresh, 1)
		s.db.Model(&Job{}).Where("id = ?", fresh.ID).Update("next_run_at", next)
		s.exe.Enqueue(fresh, "schedule")
	})
	if err != nil {
		return g.Error(err, "invalid cron %q for job %s", job.ScheduleCron, job.Name)
	}

	s.mu.Lock()
	s.ids[job.ID] = entryID
	s.tzs[job.ID] = timeKey(time.Local, job.ScheduleTZ)
	if job.ScheduleTZ != "" {
		s.tzs[job.ID] = job.ScheduleTZ
	}
	s.mu.Unlock()

	s.db.Model(&Job{}).Where("id = ?", job.ID).Update("next_run_at", s.NextRun(job, 1))
	return nil
}

// RemoveJob unregisters a job schedule.
func (s *Scheduler) RemoveJob(jobID string) {
	s.mu.Lock()
	entryID, tzName := s.ids[jobID], s.tzs[jobID]
	delete(s.ids, jobID)
	delete(s.tzs, jobID)
	s.mu.Unlock()

	if entryID == 0 {
		return
	}
	if c, ok := s.crons[tzName]; ok {
		c.Remove(entryID)
	}
}

// NextRun computes the next fire time after now (n-th occurrence).
func (s *Scheduler) NextRun(job *Job, n int) *time.Time {
	if job.ScheduleCron == "" {
		return nil
	}
	loc := time.Local
	if job.ScheduleTZ != "" {
		if l, err := time.LoadLocation(job.ScheduleTZ); err == nil {
			loc = l
		}
	}
	sched, err := cron.ParseStandard(job.ScheduleCron)
	if err != nil {
		return nil
	}
	t := sched.Next(time.Now().In(loc))
	for i := 1; i < n && !t.IsZero(); i++ {
		t = sched.Next(t)
	}
	if t.IsZero() {
		return nil
	}
	return &t
}

// Upcoming returns the next n fire times across all enabled schedules,
// soonest first (dashboard timeline).
type UpcomingItem struct {
	JobID   string    `json:"job_id"`
	JobName string    `json:"job_name"`
	At      time.Time `json:"at"`
}

func (s *Scheduler) Upcoming(n int) []UpcomingItem {
	if n <= 0 {
		n = 10
	}
	var jobs []Job
	s.db.Where("schedule_enabled = ? AND schedule_cron != ''", true).Find(&jobs)

	items := make([]UpcomingItem, 0, len(jobs))
	for i := range jobs {
		if t := s.NextRun(&jobs[i], 1); t != nil {
			items = append(items, UpcomingItem{JobID: jobs[i].ID, JobName: jobs[i].Name, At: *t})
		}
	}
	// sort soonest first, cap n
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].At.Before(items[i].At) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	if len(items) > n {
		items = items[:n]
	}
	return items
}

// Stop halts all cron instances.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.crons {
		c.Stop()
	}
}
