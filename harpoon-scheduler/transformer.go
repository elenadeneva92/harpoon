// The transformer works to make remote agents reflect the desired state
// encoded in the registry.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

type transformer struct {
	statesc chan chan map[string]agentState
	quitc   chan chan struct{}
}

func newTransformer(
	agentDiscovery agentDiscovery,
	registryPrivate registryPrivate,
	agentPollInterval time.Duration,
) *transformer {
	t := &transformer{
		statesc: make(chan chan map[string]agentState),
		quitc:   make(chan chan struct{}),
	}

	stateMachines := map[string]*stateMachine{}
	for _, endpoint := range agentDiscovery.endpoints() {
		stateMachines[endpoint] = newStateMachine(endpoint)
	}

	log.Printf("transformer: %d initial agent(s)", len(stateMachines))

	go t.loop(
		stateMachines,
		agentDiscovery,
		registryPrivate,
		agentPollInterval,
	)

	return t
}

func (t *transformer) stop() {
	q := make(chan struct{})
	t.quitc <- q
	<-q
}

// agentStates implements the agentStater interface. Since the transformer
// owns (wraps) state machines for all of the remote agents, requests for the
// current state of agents must be proxied.
func (t *transformer) agentStates() map[string]agentState {
	c := make(chan map[string]agentState)
	t.statesc <- c
	return <-c
}

func (t *transformer) loop(
	stateMachines map[string]*stateMachine,
	agentDiscovery agentDiscovery,
	registryPrivate registryPrivate,
	agentPollInterval time.Duration,
) {
	defer func() {
		for _, stateMachine := range stateMachines {
			stateMachine.stop()
		}
	}()

	agentEndpoints := make(chan []string)
	agentDiscovery.notify(agentEndpoints)
	defer agentDiscovery.stop(agentEndpoints)

	// An intermediary receives the registry states from the registry, and
	// caches the most recent one. Whenever the main runloop for the
	// transformer is ready, it receives the latest registry state. This is
	// necessary because actions we take in our main runloop may have the side
	// effect of emitting (intermediate) registry state signals back to us. If
	// we don't receive them, we can deadlock.
	updatec0 := make(chan registryState)
	registryPrivate.notify(updatec0)
	defer registryPrivate.stop(updatec0)
	updatec := make(chan registryState)
	go fwd(updatec, updatec0)

	for {
		select {
		case newAgentEndpoints := <-agentEndpoints:
			stateMachines = migrateAgents(stateMachines, newAgentEndpoints, registryPrivate)

		case update := <-updatec:
			var (
				desired = mergeRegistryStates(update.pendingSchedule, update.scheduled)
				actual  = groupByID(snapshot(stateMachines))
			)
			toSchedule, toUnschedule := diffRegistryStates(desired, actual)
			incTaskScheduleRequests(len(toSchedule))
			incTaskUnscheduleRequests(len(toUnschedule))
			for containerID, taskSpec := range toSchedule {
				// Can be made concurrent.
				log.Printf("transformer: triggering schedule %v on %s", containerID, taskSpec.Endpoint)
				registryPrivate.signal(containerID, scheduleOne(containerID, taskSpec, stateMachines, agentPollInterval))
			}
			for containerID, taskSpec := range toUnschedule {
				// Can be made concurrent.
				log.Printf("transformer: triggering unschedule %v on %s", containerID, taskSpec.Endpoint)
				registryPrivate.signal(containerID, unscheduleOne(containerID, taskSpec, stateMachines, agentPollInterval))
			}

		case c := <-t.statesc:
			c <- snapshot(stateMachines)

		case q := <-t.quitc:
			close(q)
			return
		}
	}
}

// fwd is a single-value-caching forwarder between two chans.
func fwd(dst chan<- registryState, src <-chan registryState) {
	for s := range src {
		func() {
			for {
				var ok bool
				select {
				case s, ok = <-src: // overwrite
					if !ok {
						return
					}
				case dst <- s: // successful fwd
					return
				}
			}
		}()
	}
}

func mergeRegistryStates(maps ...map[string]taskSpec) map[string]taskSpec {
	merged := map[string]taskSpec{}
	for _, m := range maps {
		for containerID, endpoint := range m {
			merged[containerID] = endpoint
		}
	}
	return merged
}

func scheduleOne(
	containerID string,
	taskSpec taskSpec,
	stateMachines map[string]*stateMachine,
	agentPollInterval time.Duration,
) schedulingSignal {
	stateMachine, ok := stateMachines[taskSpec.Endpoint]
	if !ok {
		log.Printf("transformer: %s: agent unavailable", taskSpec.Endpoint)
		return signalAgentUnavailable
	}

	if err := stateMachine.proxy().Put(containerID, taskSpec.ContainerConfig); err != nil {
		log.Printf("transformer: %s: PUT container %s failed: %s", taskSpec.Endpoint, containerID, err)
		return signalContainerPutFailed
	}

	// If we don't block and wait for it to transition from created to
	// running, a client may sneak in a second schedule request to the
	// registry before this one is complete, which will cause us to duplicate
	// our scheduling effort, which will eventually propagate a duplicate
	// signalScheduleSuccessful back to the registry, which can trigger an
	// invalid state check when it's revealed that the container ID isn't
	// pending-schedule.
	//
	// We either have to do this, or maintain state in the transformer to mark
	// container IDs that are in the process of starting up. But since I think
	// we want to support multiple transformers against the same registry, we
	// can't rely on that kind of state.
	if err := func() error {
		var (
			checkTick    = time.Tick(agentPollInterval)
			checkTimeout = time.After(time.Duration(taskSpec.ContainerConfig.Grace.Startup)*time.Second + 500*time.Millisecond)
			status       agent.ContainerStatus
		)
		for {
			select {
			case <-checkTick:
				containerInstance, err := stateMachine.proxy().Get(containerID)
				if err != nil {
					return fmt.Errorf("when making container GET: %s", err)
				}

				switch status = containerInstance.Status; status {
				case agent.ContainerStatusCreated:
					continue
				case agent.ContainerStatusRunning:
					return nil
				default:
					return fmt.Errorf("container status %s", status)
				}

			case <-checkTimeout:
				return fmt.Errorf("container status %s after %ds: timeout", status, taskSpec.ContainerConfig.Grace.Startup)
			}
		}
	}(); err != nil {
		log.Printf("transformer: %s: start container %s failed: %s", taskSpec.Endpoint, containerID, err)
		return signalContainerStartFailed
	}

	return signalScheduleSuccessful
}

func unscheduleOne(
	containerID string,
	taskSpec taskSpec,
	stateMachines map[string]*stateMachine,
	agentPollInterval time.Duration,
) schedulingSignal {
	// Unscheduling is a bit of a dance.
	//  1. POST /containers/{id}/stop
	//  2. Poll GET /containers/{id} until it's terminated
	//  3. DELETE /containers/{id}
	stateMachine, ok := stateMachines[taskSpec.Endpoint]
	if !ok {
		log.Printf("transformer: %s: agent unavailable", taskSpec.Endpoint)
		return signalAgentUnavailable
	}

	// POST stop
	if err := stateMachine.proxy().Stop(containerID); err != nil {
		log.Printf("transformer: %s: stop container %s failed: %s", taskSpec.Endpoint, containerID, err)
		return signalContainerStopFailed
	}

	// Poll GET
	if err := func() error {
		checkTick := time.Tick(agentPollInterval)
		checkTimeout := time.After(time.Duration(taskSpec.ContainerConfig.Grace.Shutdown)*time.Second + 500*time.Millisecond)
		var status agent.ContainerStatus
		for {
			select {
			case <-checkTick:
				containerInstance, err := stateMachine.proxy().Get(containerID)
				if err != nil {
					return fmt.Errorf("when making container GET: %s", err)
				}

				switch status = containerInstance.Status; status {
				case agent.ContainerStatusFailed, agent.ContainerStatusFinished:
					return nil
				default:
					continue
				}

			case <-checkTimeout:
				return fmt.Errorf("container status %s after %ds: timeout", status, taskSpec.ContainerConfig.Grace.Shutdown)
			}
		}
	}(); err != nil {
		log.Printf("transformer: %s: stop container %s failed: %s", taskSpec.Endpoint, containerID, err)
		return signalContainerStopFailed
	}

	// DELETE
	if err := stateMachine.proxy().Delete(containerID); err != nil {
		log.Printf("transformer: %s: DELETE container %s failed: %s", taskSpec.Endpoint, containerID, err)
		return signalContainerDeleteFailed
	}

	return signalUnscheduleSuccessful
}

func diffRegistryStates(
	desired map[string]taskSpec,
	actual map[string]endpointContainerInstance,
) (toSchedule, toUnschedule map[string]taskSpec) {
	toSchedule = map[string]taskSpec{}
	toUnschedule = map[string]taskSpec{}

	//log.Printf("transformer: diff(%d desired, %d actual)", len(desired), len(actual))

	// Everything which is desired may need to be scheduled.
	for containerID, desired := range desired {
		actual, ok := actual[containerID]
		if !ok {
			// The only way task instances can be lost is if their agent
			// disappears. Otherwise, we make our best effort to keep them
			// running.
			//log.Printf("transformer: %v is missing; scheduling on %s", containerID, desired.Endpoint)
			toSchedule[containerID] = desired
			continue
		}

		switch actual.Status {
		case agent.ContainerStatusCreated, agent.ContainerStatusRunning:
			// nothing to do
			//log.Printf("transformer: %v is %s on %s; nothing to do", containerID, actual.Status, actual.Endpoint)

		case agent.ContainerStatusFailed:
			//log.Printf("transformer: %v is %s on %s; will re-schedule", containerID, actual.Status, actual.Endpoint)
			toSchedule[containerID] = desired

		case agent.ContainerStatusFinished:
			// nothing to do
			//log.Printf("transformer: %v is %s on %s; nothing to do", containerID, actual.Status, actual.Endpoint)

		default:
			panic(fmt.Sprintf("container status %q has no handler in transformer diffRegistryStates", actual.Status))
		}
	}

	// Things that exist but aren't desired should be unscheduled.
	for containerID, actual := range actual {
		taskSpec := taskSpec{
			Endpoint:        actual.endpoint,
			ContainerConfig: actual.ContainerInstance.Config,
		}

		desired, ok := desired[containerID]
		if !ok {
			//log.Printf("transformer: %v exists on %s but shouldn't; unscheduling", containerID, actual.Endpoint)
			toUnschedule[containerID] = taskSpec
			continue
		}

		if desired.Endpoint != actual.endpoint {
			// move
			//log.Printf("transformer: %v exists on %s but should be on %s; unscheduling former, scheduling latter", containerID, actual.Endpoint, desired.Endpoint)
			toUnschedule[containerID] = taskSpec
			toSchedule[containerID] = desired
		}
	}

	//log.Printf("transformer: after diff, %d to schedule, %d to unschedule", len(toSchedule), len(toUnschedule))
	return toSchedule, toUnschedule
}

// migrateAgents returns a set of state machines that reflect the latest
// endpoints, re-using existing state machines when available. State machines
// that were lost (existing state machines with no corresponding new agent
// endpoint) will have all of their containers signaled as lost to the
// registry for re-scheduling.
func migrateAgents(
	existingStateMachines map[string]*stateMachine,
	newAgentEndpoints []string,
	registryPrivate registryPrivate, // to receive signals for lost containers
) map[string]*stateMachine {
	stateMachines, lostStateMachines := diffAgents(newAgentEndpoints, existingStateMachines)
	for endpoint, stateMachine := range lostStateMachines {
		containerInstances, err := stateMachine.Containers()
		if err != nil {
			log.Printf("transformer: when processing lost remote agent %s: %s", endpoint, err)
			continue
		}
		for _, containerInstance := range containerInstances {
			registryPrivate.signal(containerInstance.ID, signalContainerLost)
		}
		stateMachine.stop()
	}
	return stateMachines
}

func diffAgents(incoming []string, previous map[string]*stateMachine) (surviving, lost map[string]*stateMachine) {
	next := map[string]*stateMachine{}
	for _, endpoint := range incoming {
		if stateMachine, ok := previous[endpoint]; ok {
			next[endpoint] = stateMachine
			delete(previous, endpoint)
		} else {
			next[endpoint] = newStateMachine(endpoint)
		}
	}
	return next, previous
}

// snapshot takes a snapshot of the complete remote state.
func snapshot(stateMachines map[string]*stateMachine) map[string]agentState {
	m := map[string]agentState{}
	for endpoint, stateMachine := range stateMachines {
		hostResources, err := stateMachine.proxy().Resources()
		if err != nil {
			log.Printf("transformer: when getting host resources from %s: %s", endpoint, err)
		}
		var (
			hostResourcesDirty = err != nil
			stateMachineDirty  = stateMachine.dirty()
		)
		m[endpoint] = agentState{
			Dirty:              hostResourcesDirty || stateMachineDirty,
			HostResources:      hostResources,
			ContainerInstances: stateMachine.snapshot(),
		}
	}
	return m
}

func groupByID(snapshot map[string]agentState) map[string]endpointContainerInstance {
	m := map[string]endpointContainerInstance{}
	for endpoint, agentState := range snapshot {
		for id, instance := range agentState.ContainerInstances {
			m[id] = endpointContainerInstance{endpoint, instance}
		}
	}
	return m
}

// agentState has exported members because we serialize it for introspection
// endpoints in the scheduler.
type agentState struct {
	Dirty              bool                               `json:"dirty"` // if true, don't trust the report
	HostResources      agent.HostResources                `json:"host_resources"`
	ContainerInstances map[string]agent.ContainerInstance `json:"container_instances"`
}

type endpointContainerInstance struct {
	endpoint string
	agent.ContainerInstance
}
