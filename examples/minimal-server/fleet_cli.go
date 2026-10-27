// SPDX-License-Identifier: LGPL-3.0-only
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/thienvu18/kylok/go"
)

// Keep optional fleet/network actions in the trusted local CLI; the agent listener gains no administrator routes.
type fleetFlags struct {
	document, group, batch, operation, command, kind, secrets, notBefore, deadline *string
	revision                                                                       *uint64
	limit                                                                          *int
}

// Register explicit request identities and bounded document/window flags for the SDK examples.
func registerFleetFlags(flags *flag.FlagSet) fleetFlags {
	return fleetFlags{
		document: flags.String("document", "", "JSON membership, fleet request or network policy file"),
		group:    flags.String("group", "", "opaque group ID"), batch: flags.String("batch", "", "fleet assignment ID"),
		operation: flags.String("operation", "", "installation operation ID"), command: flags.String("command-id", "", "one-shot command ID"),
		kind: flags.String("kind", "", "one-shot REBOOT or LOCK"), secrets: flags.String("network-secrets", "", "private immutable scope/reference-to-passphrase JSON file"),
		notBefore: flags.String("not-before", "", "absolute RFC3339 deployment start"), deadline: flags.String("deadline", "", "absolute RFC3339 deployment deadline"),
		revision: flags.Uint64("group-revision", 0, "expected group revision; zero creates"), limit: flags.Int("limit", 20, "page/fan-out limit (1–100)"),
	}
}

// Recognize only explicitly implemented SDK examples before opening persistent resources.
func (f fleetFlags) accepts(command string) bool {
	switch command {
	case "set-group", "group", "groups", "assign-window", "cancel-operation", "fleet-create", "fleet", "fleets", "fleet-advance", "fleet-cancel", "command-submit", "command", "commands", "command-cancel", "set-network", "network":
		return true
	default:
		return false
	}
}

// Decode exactly one bounded document into a typed public SDK input, rejecting unknown fields and trailing data.
func readManagementDocument(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 200*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 200*1024 {
		return errors.New("management document exceeds 200 KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("one JSON document required")
	}
	return nil
}

// Invoke only public SDK APIs and print detached domain results; CLI callers own local process authorization.
func runFleetCommand(ctx context.Context, sdk *kylok.SDK, command, scope, device, digest, key, after string, timeout time.Duration, f fleetFlags) (bool, error) {
	if !f.accepts(command) {
		return false, nil
	}
	var result any
	var err error
	switch command {
	case "set-group":
		var ids []string
		if err = readManagementDocument(*f.document, &ids); err == nil {
			result, err = sdk.ReplaceGroup(ctx, scope, *f.group, ids, *f.revision)
		}
	case "group":
		result, err = sdk.GetGroup(ctx, scope, *f.group)
	case "groups":
		result, err = sdk.QueryGroups(ctx, scope, kylok.PageRequest{Cursor: after, Limit: *f.limit})
	case "assign-window":
		var start, end time.Time
		if start, err = time.Parse(time.RFC3339Nano, *f.notBefore); err != nil {
			break
		}
		if end, err = time.Parse(time.RFC3339Nano, *f.deadline); err != nil {
			break
		}
		result, err = sdk.ScheduleApplication(ctx, scope, device, digest, key, start, end)
	case "cancel-operation":
		result, err = sdk.CancelOperation(ctx, scope, device, *f.operation)
	case "fleet-create":
		var request kylok.FleetRequest
		if err = readManagementDocument(*f.document, &request); err != nil {
			break
		}
		if request.ScopeID != "" && request.ScopeID != scope {
			err = errors.New("document scope must match -scope")
			break
		}
		request.ScopeID = scope
		result, err = sdk.CreateFleetAssignment(ctx, request)
	case "fleet":
		result, err = sdk.GetFleetAssignment(ctx, scope, *f.batch)
	case "fleets":
		result, err = sdk.QueryFleetAssignments(ctx, scope, kylok.PageRequest{Cursor: after, Limit: *f.limit})
	case "fleet-advance":
		result, err = sdk.AdvanceFleetAssignment(ctx, scope, *f.batch, *f.limit)
	case "fleet-cancel":
		result, err = sdk.CancelFleetAssignment(ctx, scope, *f.batch)
	case "command-submit":
		result, err = sdk.SubmitCommand(ctx, scope, device, kylok.CommandKind(strings.ToUpper(*f.kind)), key, timeout)
	case "command":
		result, err = sdk.GetCommand(ctx, scope, device, *f.command)
	case "commands":
		result, err = sdk.QueryCommands(ctx, scope, device, kylok.PageRequest{Cursor: after, Limit: *f.limit})
	case "command-cancel":
		result, err = sdk.CancelCommand(ctx, scope, device, *f.command)
	case "set-network":
		var policy kylok.NetworkPolicy
		if err = readManagementDocument(*f.document, &policy); err == nil {
			result, err = sdk.SetNetworkPolicy(ctx, scope, device, key, policy)
		}
	case "network":
		result, err = sdk.GetNetworkPolicy(ctx, scope, device)
	}
	if err != nil {
		return true, err
	}
	return true, json.NewEncoder(os.Stdout).Encode(result)
}

// Example-only immutable vault snapshot. Operators keep this private file/versioned references consistent across restarts.
// Production hosts can supply the same public interface using their own durable encrypted vault.
type exampleNetworkSecrets struct{ values map[string]map[string]string }

// ReadNetworkSecret returns a detached scoped value without logging its content or falling back across scopes.
// Usage: value, err := provider.ReadNetworkSecret(ctx, "site", "office-key-v2").
func (s *exampleNetworkSecrets) ReadNetworkSecret(ctx context.Context, scope, reference string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, ok := s.values[scope][reference]
	if !ok {
		return nil, kylok.ErrNotFound
	}
	return []byte(value), nil
}

// Load one private immutable example vault; do not echo parser diagnostics that might include secret input.
func loadNetworkSecrets(path string) (kylok.NetworkSecretProvider, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open network secret file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("network secret file must be a private regular file (0600)")
	}
	data, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return nil, errors.New("network secret file exceeds 64 KiB or cannot be read")
	}
	var values map[string]map[string]string
	if json.Unmarshal(data, &values) != nil || values == nil {
		return nil, errors.New("invalid network secret file")
	}
	return &exampleNetworkSecrets{values: values}, nil
}
