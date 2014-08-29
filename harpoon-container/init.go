package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/docker/libcontainer"
	"github.com/docker/libcontainer/namespaces"
	"github.com/docker/libcontainer/syncpipe"
)

func init() {
	// If the process name is harpoon-container-init (set by commandBuilder in
	// container.go), execution will be hijacked from main().
	if os.Args[0] != "harpoon-container-init" {
		return
	}

	// Locking the thread here ensures that we stay in the main thread, which in
	// turn ensures that our parent death signal hasn't been reset.
	runtime.LockOSThread()

	// If the sync pipe cannot be initialized, there's no way to report an error
	// except by logging it and exiting nonzero. Once the sync pipe is set up
	// errors are communicated over it instead of through logging.
	syncPipe, err := syncpipe.NewSyncPipeFromFd(0, uintptr(3))
	if err != nil {
		fmt.Fprintf(os.Stderr, "harpoon-container: unable to initialize sync pipe: %s", err)
		os.Exit(2)
	}

	args := os.Args[1:]

	if len(args) == 0 {
		syncPipe.ReportChildError(fmt.Errorf("no command given for container"))
		os.Exit(2)
	}

	f, err := os.Open("./container.json")
	if err != nil {
		syncPipe.ReportChildError(fmt.Errorf("unable to open ./container.json: %s", err))
		os.Exit(2)
	}

	var container *libcontainer.Config

	if err := json.NewDecoder(f).Decode(&container); err != nil {
		syncPipe.ReportChildError(fmt.Errorf("unable to parse ./container.json: %s", err))
		os.Exit(2)
	}

	namespaces.Init(container, "./rootfs", "", syncPipe, args)

	// If we get past namespaces.Init(), that means the container failed to exec.
	// The error will have already been reported to the called via syncPipe, so
	// we simply exit nonzero.
	os.Exit(2)
}
