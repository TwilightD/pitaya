Typed Kick
==========

This document describes a minimal design for passing an application-defined kick type from server code to the client through a single `packet.Kick` packet.

## Goals

The framework should only transport the kick type. It should not define business meanings such as duplicate login, ban, maintenance, or server shutdown. Applications are responsible for defining the values and meanings of `kickType`.

The existing behavior must remain valid:

* `Kick(ctx)` means `kickType == 0`.
* `SendKickToUsers(uids, frontendType)` means `kickType == 0`.
* Clients that do not understand the kick payload can still disconnect when they receive a `packet.Kick`.

The new behavior should support:

* Local frontend kicks with an application-provided `kickType`.
* Backend-to-frontend RPC kicks with the same `kickType`.
* A single server-to-client packet, without sending a separate push message before kicking.

## High level flow

```mermaid
flowchart LR
  appCode["Application code"] -->|"SendKickToUsersWithType or Session.KickWithType"| pitayaServer["Pitaya server"]
  pitayaServer -->|"KickMsg: userId, kickType"| frontendServer["Frontend server"]
  frontendServer -->|"packet.Kick body: kickType"| client["Client"]
```

For a local session on the frontend server, the call goes directly from `Session.KickWithType` to `agent.KickWithType`.

For a remote session, the backend server sends `KickMsg` to the frontend server. The frontend server finds the session and calls `Session.KickWithType` with the value from `KickMsg`.

## Protocol changes

Add `kickType` to `KickMsg` in `pitaya-protos/kick.proto`:

```proto
message KickMsg {
  string userId = 1;
  int32 kickType = 2;
}
```

Regenerate the Go protobuf files after changing the proto:

```shell
make protos-compile
```

The client-facing `packet.Kick` body should contain the `kickType`. Use a fixed-width encoding so the client can parse it consistently.

Recommended encoding:

* 4 bytes.
* Big-endian.
* Signed `int32`.

This avoids constraining applications to `0..255` and keeps the framework neutral about business-defined ranges.

## Server API changes

Keep the old APIs and add typed variants.

Add to `session.Session`:

```go
KickWithType(ctx context.Context, kickType int32) error
```

`Kick(ctx)` should call:

```go
return s.KickWithType(ctx, 0)
```

Add to `networkentity.NetworkEntity`:

```go
KickWithType(ctx context.Context, kickType int32) error
```

`sessionImpl.KickWithType` should call the underlying entity and then close the session on success, preserving the current close behavior:

```go
func (s *sessionImpl) KickWithType(ctx context.Context, kickType int32) error {
	if err := s.entity.KickWithType(ctx, kickType); err != nil {
		return err
	}

	s.Close()
	return nil
}
```

`agentImpl.KickWithType` should always encode the provided `kickType` into the `packet.Kick` body. It should not define or validate business meanings.

`Remote.Kick(ctx)` should keep the old behavior by calling `Remote.KickWithType(ctx, 0)`. `Remote.KickWithType` should marshal `KickMsg` with both `UserId` and `KickType`.

## Application API changes

Keep:

```go
SendKickToUsers(uids []string, frontendType string) ([]string, error)
```

Add:

```go
SendKickToUsersWithType(uids []string, frontendType string, kickType int32) ([]string, error)
```

The old method should delegate to the new method with `kickType == 0`.

For local sessions, call:

```go
s.KickWithType(context.Background(), kickType)
```

For remote sessions, send:

```go
&protos.KickMsg{
	UserId:   uid,
	KickType: kickType,
}
```

Add the same method to the public `Pitaya` interface and the static helper layer.

## RPC handling

Update the RPC receivers to preserve the value from `KickMsg`:

* `RemoteService.KickUser` should call `s.KickWithType(ctx, kick.GetKickType())`.
* `remote.Sys.Kick` should call `sess.KickWithType(ctx, msg.GetKickType())`.

This keeps local, NATS, and gRPC kick paths consistent.

## Client behavior

The client should parse the `packet.Kick` body before disconnecting.

Recommended behavior:

* Empty body: treat as `kickType == 0`.
* 4-byte body: parse as big-endian `int32`.
* Unknown `kickType`: use a generic kicked/disconnected handling path.
* Parsing failure: fall back to `kickType == 0` and disconnect.

The client should not rely on Pitaya defining the business meaning. The application should map values to user-facing behavior.

The Go client exposes received kick types through `Client.KickChannel()` before disconnecting. Application clients can read from this channel and map the value to their own user-facing handling.

## Tests to update

Update generated mocks after changing interfaces:

```shell
make agent-mock session-mock networkentity-mock pitaya-mock
```

Update or add tests in these areas:

* `agent`: `Kick(ctx)` writes `kickType == 0`; `KickWithType(ctx, X)` writes `X` into the kick packet body; existing encode and write error tests continue to pass.
* `session`: `Kick(ctx)` delegates to default typed kick; `KickWithType(ctx, X)` calls the entity typed kick and still closes the session on success.
* `kick.go`: old `SendKickToUsers` keeps default behavior; new `SendKickToUsersWithType` passes the provided type to local sessions and remote `KickMsg`.
* `service/remote.go` and `remote/sys.go`: `KickMsg.KickType` reaches `Session.KickWithType`.
* `agent_remote.go`: `Remote.KickWithType` sends `KickMsg.KickType`.
* `static.go`: static helper forwards the provided `kickType`.

Suggested focused test command:

```shell
go test ./agent ./session ./service ./remote .
```

After generated files are updated, run the full suite if practical:

```shell
go test ./...
```

## Compatibility notes

This change is backward compatible at the API level because existing methods stay in place and continue to use `kickType == 0`.

Wire compatibility depends on client behavior. Old clients that ignore the `packet.Kick` body should continue to disconnect. New clients should tolerate an empty body so they can talk to old servers.

The framework should not reject unknown `kickType` values. Range ownership belongs to the application.
