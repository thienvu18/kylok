// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"context"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

var ErrUnsupported = ports.ErrUnsupported

// CommandKind selects a one-shot Device Owner action, subject to platform capability and observed outcome.
type CommandKind string

const (
	CommandReboot CommandKind = "REBOOT"
	CommandLock   CommandKind = "LOCK"
)

// CommandObservation is platform readback, not proof of who caused a reboot or cryptographic device attestation.
type CommandObservation = ports.CommandObservation

// Command exposes a one-shot lifecycle without the agent's dispatch credential.
// UNCERTAIN requires an explicit host decision; retrying the same key never schedules another action.
type Command struct {
	ID          string              `json:"id"`
	DeviceID    string              `json:"device_id"`
	Kind        CommandKind         `json:"kind"`
	State       string              `json:"state"`
	Deadline    time.Time           `json:"deadline"`
	ErrorCode   string              `json:"error_code,omitempty"`
	Observation *CommandObservation `json:"observation,omitempty"`
}

// CommandPage contains a live ascending-ID history page for one authorized device.
type CommandPage struct {
	Items      []Command
	NextCursor string
}

// SubmitCommand queues reboot or lock only when the device advertises the corresponding Device Owner capability.
// A stable key binds device/kind; its original deadline is retained on retry (timeout 1m–7d).
// Reboot success needs a boot-count increase; lock success needs a configured secure lock and locked readback.
// Usage: command, err := sdk.SubmitCommand(ctx, scope, deviceID, kylok.CommandReboot, "maintenance", time.Hour).
func (s *SDK) SubmitCommand(ctx context.Context, scope, device string, kind CommandKind, key string, timeout time.Duration) (Command, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Command{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || !idPattern.MatchString(key) || kind != CommandReboot && kind != CommandLock || timeout < time.Minute || timeout > 7*24*time.Hour {
		return Command{}, ErrInvalidArgument
	}
	id, err := secret()
	if err != nil {
		return Command{}, err
	}
	now := s.clock().UTC().Truncate(time.Millisecond)
	command, err := s.store.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: scope, DeviceID: device, Kind: string(kind), IdempotencyKey: key, ID: id, Now: now, Deadline: now.Add(timeout)})
	return publicCommand(command), err
}

// GetCommand reads an authorized device's one-shot outcome; overdue delivered actions are UNCERTAIN, never replayed.
// Usage: command, err := sdk.GetCommand(ctx, scope, deviceID, commandID).
func (s *SDK) GetCommand(ctx context.Context, scope, device, id string) (Command, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Command{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || !devicePattern.MatchString(id) {
		return Command{}, ErrInvalidArgument
	}
	command, err := s.store.GetCommand(ctx, scope, device, id, s.clock().UTC())
	return publicCommand(command), err
}

// QueryCommands reads bounded command history with a scope/device-bound cursor; it never triggers a platform action.
// Usage: page, err := sdk.QueryCommands(ctx, scope, deviceID, PageRequest{Limit:20}).
func (s *SDK) QueryCommands(ctx context.Context, scope, device string, page PageRequest) (CommandPage, error) {
	if err := s.checkOpen(ctx); err != nil {
		return CommandPage{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) {
		return CommandPage{}, ErrInvalidArgument
	}
	binding := cursorBinding("commands", scope, device, "", "")
	after, limit, err := readPage(page, binding)
	if err != nil {
		return CommandPage{}, err
	}
	values, err := s.store.ListCommands(ctx, scope, device, after, limit+1, s.clock().UTC())
	if err != nil {
		return CommandPage{}, err
	}
	result := CommandPage{}
	if len(values) > limit {
		result.NextCursor = nextPage(binding, values[limit-1].ID)
		values = values[:limit]
	}
	for _, command := range values {
		result.Items = append(result.Items, publicCommand(command))
	}
	return result, nil
}

// CancelCommand cancels queued work; committed delivery becomes UNCERTAIN until a valid platform result resolves it.
// Cancellation cannot undo a delivered reboot/lock, and never schedules a replacement action.
// Usage: command, err := sdk.CancelCommand(ctx, scope, deviceID, commandID).
func (s *SDK) CancelCommand(ctx context.Context, scope, device, id string) (Command, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Command{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || !devicePattern.MatchString(id) {
		return Command{}, ErrInvalidArgument
	}
	command, err := s.store.CancelCommand(ctx, scope, device, id, s.clock().UTC())
	return publicCommand(command), err
}

// Strip the stable dispatch token and detach the observation before returning host domain data.
func publicCommand(value ports.Command) Command {
	result := Command{ID: value.ID, DeviceID: value.DeviceID, Kind: CommandKind(value.Kind), State: value.State, Deadline: value.Deadline, ErrorCode: value.ErrorCode}
	if value.Observation != nil {
		observed := *value.Observation
		result.Observation = &observed
	}
	return result
}
