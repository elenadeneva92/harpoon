package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"
)

var (
	heartbeatInterval = 3 * time.Second

	addr              = flag.String("addr", ":3333", "address to listen on")
	configuredVolumes = volumes{}

	agentTotalMem int64
	agentTotalCPU int64

	hostname string

	logAddr = flag.String("log.addr", ":3334", "address for log communications")
)

func init() {
	name, err := os.Hostname()
	if err != nil {
		log.Fatal("unable to get hostname: ", err)
	}
	hostname = name
}

func main() {
	flag.Int64Var(&agentTotalCPU, "cpu", -1, "available cpu resources (-1 to use all cpus)")
	flag.Int64Var(&agentTotalMem, "mem", -1, "available memory resources in MB (-1 to use all)")
	flag.Var(&configuredVolumes, "v", "repeatable list of available volumes")
	flag.Parse()

	if agentTotalCPU == -1 {
		agentTotalCPU = systemCPUs()
	}

	if agentTotalMem == -1 {
		mem, err := systemMemoryMB()
		if err != nil {
			log.Fatal("unable to get available memory: ", err)
		}

		agentTotalMem = mem
	}

	var (
		r   = newRegistry()
		api = newAPI(r)
	)

	go receiveLogs(r)

	http.Handle("/", api)

	go func() {
		// recover our state from disk
		recoverContainers(r)

		// begin accepting runner updates
		r.acceptStateUpdates()

		if r.len() > 0 {
			// wait for runners to check in
			time.Sleep(3 * heartbeatInterval)
		}

		api.enable()
	}()

	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

type volumes map[string]struct{}

func (*volumes) String() string           { return "" }
func (v *volumes) Set(value string) error { (*v)[value] = struct{}{}; return nil }

// not implemented yet
func recoverContainers(r *registry) {}
