package main

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

var ok = struct{}{}

func TestMatch(t *testing.T) {
	var (
		cfg            = newConfig(300, 2, map[string]string{"/a": "", "/b": ""})
		validResources = []freeResources{
			freeResources{cpus: 2, memory: 300, volumes: toSet([]string{"/a", "/b", "/c"})},
			freeResources{cpus: 2, memory: 400, volumes: toSet([]string{"/a", "/b"})},
			freeResources{cpus: 3, memory: 300, volumes: toSet([]string{"/a", "/b"})},
			freeResources{cpus: 4, memory: 300, volumes: toSet([]string{"/a", "/b", "/c"})},
			freeResources{cpus: 4, memory: 300, volumes: toSet([]string{"/a", "/b", "/c"})},
		}
		invalidResources = []freeResources{
			freeResources{cpus: 1, memory: 300, volumes: toSet([]string{"/a", "/b"})},
			freeResources{cpus: 2, memory: 200, volumes: toSet([]string{"/a", "/b"})},
			freeResources{cpus: 2, memory: 200, volumes: toSet([]string{"/a", "/b"})},
			freeResources{cpus: 100, memory: 1100, volumes: toSet([]string{"/a", "/c"})},
			freeResources{cpus: 2, memory: 200, volumes: toSet([]string{"/b"})},
			freeResources{cpus: 2, memory: 200, volumes: toSet([]string{"/a"})},
			freeResources{cpus: 2, memory: 200, volumes: toSet([]string{"/a"})},
		}
	)

	for _, resources := range validResources {
		if !match(cfg, resources) {
			t.Errorf("error %v should match to the resources %v ", cfg, resources)
		}
	}

	for _, resources := range invalidResources {
		if match(cfg, resources) {
			t.Errorf("error %v should not match to the resources %v ", cfg, resources)
		}
	}

}

func TestFilter(t *testing.T) {
	var resources = map[string]freeResources{
		"state1": freeResources{
			cpus:    3,
			memory:  700,
			volumes: toSet([]string{"/a", "/b", "/c"}),
		},
		"state2": freeResources{
			cpus:    11,
			memory:  200,
			volumes: toSet([]string{"/a", "/c"}),
		},
		"state3": freeResources{
			cpus:    1,
			memory:  1,
			volumes: toSet([]string{}),
		},
		"state4": freeResources{
			cpus:    3,
			memory:  700,
			volumes: toSet([]string{"/b"}),
		},
	}

	validAgents := filter(newConfig(1100, 12, map[string]string{}), resources)
	if len(validAgents) != 0 {
		t.Errorf("found agent for config with infeasible resources")
	}

	validAgents = filter(newConfig(300, 2, map[string]string{"/a": "", "/b": ""}), resources)
	if expected, actual := 1, len(validAgents); actual != expected {
		t.Fatalf("number of valid agents found: actual %d != expected %d", actual, expected)
	}
	if validAgents[0] != "state1" {
		t.Error("missing valid agent after filtering")
	}

	resources["state"] = freeResources{
		cpus:    100,
		memory:  10000,
		volumes: toSet([]string{}),
	}
	validAgents = filter(newConfig(1, 1, map[string]string{}), resources)
	if expected, actual := len(resources), len(validAgents); actual != expected {
		t.Fatalf("number of valid agents found: actual %d != expected %d", actual, expected)
	}

	for _, agent := range validAgents {
		if _, ok := resources[agent]; !ok {
			t.Errorf("unexpected agent after filter %s", agent)
		}
		delete(resources, agent)
	}
}

func TestRandomFit(t *testing.T) {
	var (
		cfgs = map[string]agent.ContainerConfig{
			"cfg1": newConfig(300, 2, map[string]string{"/a": "", "/b": ""}),
			"cfg2": newConfig(300, 3, map[string]string{"/c": "", "/b": ""}),
			"cfg3": newConfig(1, 4, map[string]string{}),
			"cfg4": newConfig(1, 4, map[string]string{}),
			"cfg5": newConfig(1, 4, map[string]string{}),
			"cfg6": newConfig(1100, 12, map[string]string{}),
		}
		states = map[string]agentState{
			"state1": agentState{
				resources: freeResources{
					cpus:    3,
					memory:  700,
					volumes: toSet([]string{"/a", "/b", "/c"}),
				},
			},
			"state2": agentState{
				resources: freeResources{
					cpus:    11,
					memory:  200,
					volumes: toSet([]string{"/a", "/c"}),
				},
			},
			"state3": agentState{
				resources: freeResources{
					cpus:    1,
					memory:  1,
					volumes: toSet([]string{}),
				},
			},
			"state4": agentState{
				resources: freeResources{
					cpus:    3,
					memory:  700,
					volumes: toSet([]string{"/b"}),
				},
			},
		}
		expectedMapping = []struct {
			name           string
			scheduledTasks int
			possibleTasks  map[string]struct{}
		}{
			{"state1", 1, map[string]struct{}{"cfg1": ok, "cfg2": ok}},
			{"state2", 2, map[string]struct{}{"cfg3": ok, "cfg4": ok, "cfg5": ok}},
		}
	)

	mapping, unscheduled := randomFit(cfgs, states, map[string]pendingTask{})
	if len(mapping) != len(expectedMapping) {
		t.Fatalf("wrong count of agents with scheduled tasks: actual %d != expected %d", len(mapping), len(expectedMapping))
	}
	var (
		_, unscheduledCfg1 = unscheduled["cfg1"]
		_, unscheduledCfg2 = unscheduled["cfg2"]
	)
	if unscheduledCfg1 == unscheduledCfg2 {
		if unscheduledCfg1 {
			t.Fatal("configs [cfg1, cfg2] should not be both unscheduled")
		} else {
			t.Fatal("configs [cfg1, cfg2] should not be both scheduled")
		}
	}

	var (
		_, unscheduledCfg3 = unscheduled["cfg3"]
		_, unscheduledCfg4 = unscheduled["cfg4"]
		_, unscheduledCfg5 = unscheduled["cfg5"]
	)

	if !unscheduledCfg3 && !unscheduledCfg4 && !unscheduledCfg5 {
		t.Fatalf("one of the config (cfg3, cfg4, cfg5) should be unscheduled: unscheduled (%v, %v, %v)",
			unscheduledCfg3,
			unscheduledCfg4,
			unscheduledCfg5,
		)
	}

	if _, unscheduledCfg6 := unscheduled["cfg6"]; !unscheduledCfg6 {
		t.Fatalf("Task cfg6 should not be scheduled!")
	}

	state3 := "state3"
	tasks, ok := mapping[state3]
	if ok {
		t.Fatalf("agent %q should not have scheduled tasks but have %v", state3, tasks)
	}

	if expectedUnscheduledCfgs := 3; len(unscheduled) != expectedUnscheduledCfgs {
		t.Fatalf("unscheduled task count: actual %d != expected %d", len(unscheduled), expectedUnscheduledCfgs)
	}

	for _, agent := range expectedMapping {
		tasks := mapping[agent.name]
		if len(tasks) != agent.scheduledTasks {
			t.Fatalf("Wrong schedule agent: %v actual %d != expected %d", agent, len(tasks), agent.scheduledTasks)
		}
		for name, config := range tasks {
			if _, ok := agent.possibleTasks[name]; !ok {
				t.Fatalf("Task %s should not be schedule on agent %s", name, agent.name)
			}
			if !reflect.DeepEqual(config, cfgs[name]) {
				t.Fatalf("Not right configuration %s returned actual %v != expected %v", name, config, cfgs[name])
			}
		}
	}
}

func TestRandomFitWithPendingTasks(t *testing.T) {
	var (
		cfgs   = map[string]agent.ContainerConfig{}
		states = map[string]agentState{
			"state1": agentState{
				resources: freeResources{
					cpus:    5.5,
					memory:  1100,
					volumes: toSet([]string{"/a", "/b", "/c"}),
				},
			},
		}
		pendingTasks = map[string]pendingTask{}
	)

	for i := 0; i < 11; i++ {
		cfgs[fmt.Sprintf("cfg%d", i)] = newConfig(100, 0.5, map[string]string{"/a": "", "/b": ""})

		id := fmt.Sprintf("cfg1%d", i)
		pendingTasks[id] = pendingTask{
			id:       id,
			endpoint: "state1",
			cfg: agent.ContainerConfig{
				Resources: agent.Resources{
					CPUs:   0.5,
					Memory: 100,
				},
			},
		}
	}

	mapping, unscheduled := randomFit(cfgs, states, pendingTasks)
	fmt.Println(mapping)
	if want, have := 0, len(mapping); want != have {
		t.Errorf("not right count of scheduled tasks: expected %d != actual %d", want, have)
	}

	if want, have := len(cfgs), len(unscheduled); want != have {
		t.Errorf("not right count of unscheduled tasks: expected %d != actual %d", want, have)
	}

	states["state2"] = agentState{
		resources: freeResources{
			cpus:    5.5,
			memory:  1100,
			volumes: toSet([]string{"/a", "/b", "/c"}),
		},
	}

	mapping, unscheduled = randomFit(cfgs, states, pendingTasks)
	if want, have := 1, len(mapping); want != have {
		t.Errorf("not right count of scheduled tasks: expected %d != actual %d", want, have)
	}

	if want, have := 0, len(unscheduled); want != have {
		t.Errorf("not right count of unscheduled tasks: expected %d != actual %d", want, have)
	}

	if instances, ok := mapping["state2"]; !ok || len(instances) != len(cfgs) {
		t.Fatalf("On agent should be all cfg ")
	}

}

func TestRandomFitWithoutResources(t *testing.T) {
	var (
		cfgs   = map[string]agent.ContainerConfig{}
		states = map[string]agentState{}
	)

	mapping, unscheduled := randomFit(cfgs, states, map[string]pendingTask{})
	if expected, actual := 0, len(unscheduled); actual != expected {
		t.Fatalf("unscheduled task count: actual %d != expected %d", actual, expected)
	}
	if expected, actual := 0, len(mapping); actual != expected {
		t.Fatalf("empty config should not return any mapping %v", mapping)
	}

	cfgs["random1"] = newConfig(100, 12, map[string]string{"/a": ""})
	cfgs["random2"] = newConfig(100, 12, map[string]string{})

	mapping, unscheduled = randomFit(cfgs, states, map[string]pendingTask{})
	if expected, actual := 0, len(mapping); actual != expected {
		t.Fatalf("unscheduled task count: actual %d != expected %d", actual, expected)
	}

	if !reflect.DeepEqual(cfgs, unscheduled) {
		t.Fatalf("incorrect unscheduled tasks returned")
	}
}

func newConfig(memory int, cpus float64, volumes map[string]string) agent.ContainerConfig {
	return agent.ContainerConfig{
		Resources: agent.Resources{
			Memory: memory,
			CPUs:   cpus,
		},
		Storage: agent.Storage{
			Volumes: volumes,
		},
	}
}
