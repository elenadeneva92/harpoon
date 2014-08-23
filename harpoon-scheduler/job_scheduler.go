package main

/*
import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
	"github.com/soundcloud/harpoon/harpoon-configstore/lib"
)

var (
	algorithm = randomChoice
)

type jobScheduler interface {
	schedule(configstore.JobConfig) error
	unschedule(configstore.JobConfig) error
	migrate(from, to configstore.JobConfig) error
}

type realJobScheduler struct {
	schedc    chan schedJobReq
	unschedc  chan schedJobReq
	migratec  chan migrateJobReq
	snapshotc chan map[string]map[string]agent.ContainerInstance
	quitc     chan chan struct{}
}

var _ jobScheduler = &realJobScheduler{}

func newRealJobScheduler(actual actualBroadcaster, target taskScheduler) *realJobScheduler {
	s := &realJobScheduler{
		schedc:    make(chan schedJobReq),
		unschedc:  make(chan schedJobReq),
		migratec:  make(chan migrateJobReq),
		snapshotc: make(chan map[string]map[string]agent.ContainerInstance),
		quitc:     make(chan chan struct{}),
	}

	go s.loop(actual, target)

	return s
}

func (s *realJobScheduler) schedule(job configstore.JobConfig) error {
	if err := job.Valid(); err != nil {
		return err
	}

	req := schedJobReq{job, make(chan error)}

	s.schedc <- req

	return <-req.err
}

func (s *realJobScheduler) unschedule(job configstore.JobConfig) error {
	if err := job.Valid(); err != nil {
		return err
	}

	req := schedJobReq{job, make(chan error)}

	s.unschedc <- req

	return <-req.err
}

func (s *realJobScheduler) migrate(from, to configstore.JobConfig) error {
	if err := from.Valid(); err != nil {
		return err
	}

	if err := to.Valid(); err != nil {
		return err
	}

	req := migrateJobReq{from, to, make(chan error)}

	s.migratec <- req

	return <-req.err
}

func (s *realJobScheduler) quit() {
	q := make(chan struct{})
	s.quitc <- q
	<-q
}

func (s *realJobScheduler) loop(actual actualBroadcaster, target taskScheduler) {
	var (
		updatec = make(chan map[string]map[string]agent.ContainerInstance)
		current = map[string]map[string]agent.ContainerInstance{}
	)

	actual.subscribe(updatec)
	defer actual.unsubscribe(updatec)

	select {
	case current = <-updatec:
	case <-time.After(time.Millisecond):
		panic("misbehaving actual-state broadcaster")
	}

	for {
		select {
		case req := <-s.schedc:
			req.err <- scheduleJob(req.JobConfig, current, target)

		case req := <-s.unschedc:
			req.err <- unscheduleJob(req.JobConfig, current, target)

		case req := <-s.migratec:
			req.err <- migrateJob(req.from, req.to, current, target)

		case current = <-updatec:

		case q := <-s.quitc:
			close(q)
			return
		}
	}
}

func scheduleJob(jobConfig configstore.JobConfig, current map[string]map[string]agent.ContainerInstance, target taskScheduler) error {
	log.Printf("job scheduler: request to schedule %s", jobConfig.Job)
	incJobScheduleRequests(1)

	specs, err := algorithm(jobConfig, current)
	if err != nil {
		return err
	}

	if len(specs) <= 0 {
		return fmt.Errorf("job contained no tasks")
	}

	incContainersPlaced(len(specs))

	undo := []func(){}

	defer func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}()

	for _, spec := range specs {
		if err := target.schedule(spec); err != nil {
			return err
		}

		undo = append(undo, func() { target.unschedule(spec.Endpoint, spec.ContainerID) })
	}

	undo = []func(){}

	return nil
}

func unscheduleJob(jobConfig configstore.JobConfig, actual map[string]map[string]agent.ContainerInstance, target taskScheduler) error {
	log.Printf("job scheduler: request to unschedule %s", jobConfig.Job)
	incJobUnscheduleRequests(1)

	var (
		targets = map[string]string{}
	)

	for i := 0; i < jobConfig.Scale; i++ {
		targets[makeContainerID(jobConfig, i)] = jobConfig.Job
	}

	var (
		orig = len(targets)
		undo = []func(){}
	)

	defer func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}()

	for endpoint, instances := range actual {
		for id, instance := range instances {
			if job, ok := targets[id]; ok {
				if err := target.unschedule(endpoint, id); err != nil {
					return err
				}

				undo = append(undo, func() {
					target.schedule(taskSpec{
						Endpoint:        endpoint,
						Job:             job,
						ContainerID:     id,
						ContainerConfig: instance.Config,
					})
				})

				delete(targets, id)
			}
		}
	}

	if len(targets) >= orig {
		return fmt.Errorf("job not scheduled")
	}

	if len(targets) > 0 {
		log.Printf("scheduler: unschedule job: failed to find %d container(s) (%v)", len(targets), targets)
	}

	undo = []func(){}

	return nil
}

func migrateJob(from, to configstore.JobConfig, current map[string]map[string]agent.ContainerInstance, target taskScheduler) error {
	return fmt.Errorf("not yet implemented")
}

func makeContainerID(cfg configstore.JobConfig, i int) string {
	return fmt.Sprintf("%s-%s-%d", cfg.Job, refHash(cfg), i)
}

func refHash(v interface{}) string {
	// TODO(pb): need stable encoding, either not-JSON (most likely), or some
	// way of getting stability out of JSON.

	h := md5.New()

	if err := json.NewEncoder(h).Encode(v); err != nil {
		panic(fmt.Sprintf("%s: refHash error: %s", reflect.TypeOf(v), err))
	}

	return fmt.Sprintf("%x", h.Sum(nil))[:7]
}

type schedJobReq struct {
	configstore.JobConfig
	err chan error
}

type migrateJobReq struct {
	from, to configstore.JobConfig
	err      chan error
}
*/
