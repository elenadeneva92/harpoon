# Agent API

This document describes the v0 draft of the agent API.
All paths should be prefixed with `/api/v0`.


## PUT /containers/{id}

Uploads the config to the agent, making it available for start, stop, etc.
operations. Body should be a JSON-encoded [ContainerConfig][containerconfig].
Returns 201 (Created) on success. The container will be started.


## GET /containers/{id}

Returns a JSON-encoded [ContainerInstance][containerinstance].


## POST /containers/{id}/{action}

### POST /containers/{id}/start

Starts the container. Does nothing if the container is already running.
Returns immediately with 202 (Accepted) if the container exists. If the
container doesn't start within the startup grace period specified in the
[TaskConfig][taskconfig], the agent is free to forcefully terminate the
container. To check if a started container is running, poll `GET
/containers/{id}`.

### POST /containers/{id}/stop

Stops the container. Sends SIGTERM, and waits for the container to exit. If
the container doesn't stop with in the shutdown grace period specified in the
[TaskConfig][taskconfig], sends SIGKILL. Returns immediately with 202
(Accepted) if the container exists.

Note that a stopped container still retains its resource reservations. To get
rid of those, issue a delete.

### PUT /containers/{id}?replace={old_id}

Replace an existing container with a new one. Request body should be the
container configuration. Returns immediately with 202 (Accepted) if the
configuration is valid, the host has sufficient resources, and `{old_id}`
exists and is running.

The new container will be initialized and started. If it is successful, the old
container will be destroyed. If the new container is unable to start, it will
enter a failed state and the old container will be unchanged.

This method is designed to be used by schedulers other than harpoon-scheduler.
Specifically, it's intended to provide a safer upgrade process for stateful
services.

## DELETE /containers/{id}

Destroys a container. Frees any resources associated with the container. Fails
with if the container is currently running. Returns 200 (OK) on success.

## GET /containers

Returns an array of [ContainerInstance][containerinstance] objects,
representing the current state of the agent.

If the request header `Accept: text/event-stream` is provided, the agent will
instead yield a stream of container events, as [server-sent events][sse]. We
use eventsource events because there's a proper spec, it's supported by
browsers, and it provides a nice upgrade and enhancement path if we want to
supply additional fields or metadata.

[sse]: http://www.w3.org/TR/eventsource

Events are always JSON arrays containing ContainerInstances. The first event
on every request contains every ContainerInstance in the agent. Subsequent
events contain ContainerInstances for any container that changes state.

```
data: [...]

data: [...]
```

## GET /containers/{id}/log?history=10

Returns history log lines from the container.

If the request header `Accept: text/event-stream` is provided, the agent will
instead yield a stream of [eventstream data events][sse] representing the log
lines for that container.

```
data: Log line one

data: Log line two
```

## GET /resources

Returns [HostResources][hostresources] information.


[containerconfig]: http://godoc.org/github.com/soundcloud/harpoon/harpoon-agent/lib#ContainerConfig
[containerinstance]: http://godoc.org/github.com/soundcloud/harpoon/harpoon-agent/lib#ContainerInstance
[hostresources]: http://godoc.org/github.com/soundcloud/harpoon/harpoon-agent/lib#HostResources
[taskconfig]: http://godoc.org/github.com/soundcloud/harpoon/harpoon-configstore/lib#TaskConfig
