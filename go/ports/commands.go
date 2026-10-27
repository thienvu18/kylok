// SPDX-License-Identifier: LGPL-3.0-only
package ports

import (
	"context"
	"time"
)

// Command is a one-shot Android action. DispatchToken binds reports and never changes on retry.
// DISPATCHED means delivery was committed; UNCERTAIN never authorizes automatic replay.
// A successful reboot requires a boot-count increase; lock success requires secure, locked readback.
type Command struct {
	ID            string              `json:"id"`
	DeviceID      string              `json:"device_id"`
	Kind          string              `json:"kind"`
	State         string              `json:"state"`
	DispatchToken string              `json:"dispatch_token,omitempty"`
	Deadline      time.Time           `json:"deadline"`
	ErrorCode     string              `json:"error_code,omitempty"`
	Observation   *CommandObservation `json:"observation,omitempty"`
}

// CommandObservation carries platform readback; transport reconnection and setter return are not observations.
type CommandObservation struct {
	BootBefore int64 `json:"boot_before"`
	BootAfter  int64 `json:"boot_after"`
	Secure     bool  `json:"secure"`
	Locked     bool  `json:"locked"`
}

// CommandAssignment binds the device/kind to a stable host request key before any dispatch.
type CommandAssignment struct {
	ScopeID, DeviceID, Kind, IdempotencyKey, ID string
	Now, Deadline                               time.Time
}

// CommandReport is a durable terminal result bound to the original command dispatch token.
// Exact repeats are accepted; changed reports for the same command conflict.
type CommandReport struct {
	CommandID, DispatchToken, State, ErrorCode string
	Observation                                *CommandObservation
}

// CommandStore shares authentication, serialization and transactional changes with the agent state store.
type CommandStore interface {
	// SubmitCommand binds a capability-gated reboot/lock request, with at most sixteen active commands per device.
	// Usage: command, err := store.SubmitCommand(ctx, assignment).
	SubmitCommand(context.Context, CommandAssignment) (Command, error)
	// GetCommand reads a scoped result and expires queued/dispatched work as FAILED/UNCERTAIN respectively.
	// Usage: command, err := store.GetCommand(ctx, scope, deviceID, commandID, now).
	GetCommand(context.Context, string, string, string, time.Time) (Command, error)
	// ListCommands pages one device's command history, expiring overdue work before the read.
	// Usage: commands, err := store.ListCommands(ctx, scope, deviceID, afterID, 20, now).
	ListCommands(context.Context, string, string, string, int, time.Time) ([]Command, error)
	// CancelCommand cancels only undispatched work; dispatched work becomes UNCERTAIN because it may have executed.
	// Usage: command, err := store.CancelCommand(ctx, scope, deviceID, commandID, now).
	CancelCommand(context.Context, string, string, string, time.Time) (Command, error)
}
