// SPDX-License-Identifier: LGPL-3.0-only
package ports

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type Policy struct {
	RequiredPackages        []string          `json:"required_packages,omitempty"`
	LockTask                *LockTaskPolicy   `json:"lock_task,omitempty"`
	CameraDisabled          *bool             `json:"camera_disabled,omitempty"`
	ScreenCaptureDisabled   *bool             `json:"screen_capture_disabled,omitempty"`
	Restrictions            []RestrictionRule `json:"restrictions,omitempty"`
	Permissions             []PermissionRule  `json:"permissions,omitempty"`
	MaximumTimeToLockMillis *int64            `json:"maximum_time_to_lock_millis,omitempty"`
}
type LockTaskPolicy struct {
	Packages     []string `json:"packages"`
	KioskPackage string   `json:"kiosk_package"`
}
type RestrictionRule struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}
type PermissionRule struct {
	PackageName string `json:"package_name"`
	Permission  string `json:"permission"`
	State       string `json:"state"`
}
type DesiredPolicy struct {
	Revision uint64 `json:"revision"`
	Policy   Policy `json:"policy"`
}
type PolicyAssignment struct {
	ScopeID, DeviceID, IdempotencyKey string
	Policy                            Policy
}
type PolicyResult struct {
	RuleID        string `json:"rule_id"`
	State         string `json:"state"`
	Reason        string `json:"reason"`
	ObservedValue string `json:"observed_value"`
}
type PolicyReport struct {
	Revision         uint64         `json:"revision"`
	SnapshotRevision uint64         `json:"snapshot_revision"`
	Results          []PolicyResult `json:"results"`
}
type PolicyStatus struct {
	Desired    *DesiredPolicy `json:"desired,omitempty"`
	Reported   *PolicyReport  `json:"reported,omitempty"`
	Compliance []PolicyResult `json:"compliance"`
}

var packagePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)
var restrictions = []string{"no_add_user", "no_debugging_features", "no_modify_accounts", "no_uninstall_apps"}

// NormalizePolicy validates and copies the document, sorting unordered sets for
// stable idempotency. A setting's absence differs from an explicit false/zero.
// Usage: normalized, err := ports.NormalizePolicy(policy).
func NormalizePolicy(input Policy) (Policy, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	if err = json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	validPackage := func(name string) bool { return len(name) <= 256 && packagePattern.MatchString(name) }
	validateSet := func(values []string) error {
		if len(values) > 64 {
			return errors.New("at most 64 packages per policy set")
		}
		sort.Strings(values)
		for i, v := range values {
			if !validPackage(v) || i > 0 && values[i-1] == v {
				return errors.New("invalid or duplicate package")
			}
		}
		return nil
	}
	if err = validateSet(p.RequiredPackages); err != nil {
		return p, err
	}
	if p.LockTask != nil {
		if p.LockTask.Packages == nil {
			p.LockTask.Packages = []string{}
		}
		if err = validateSet(p.LockTask.Packages); err != nil {
			return p, err
		}
		for _, name := range p.LockTask.Packages {
			if !slices.Contains(p.RequiredPackages, name) {
				return p, errors.New("Lock Task packages must be required packages")
			}
		}
		if p.LockTask.KioskPackage != "" && !slices.Contains(p.LockTask.Packages, p.LockTask.KioskPackage) {
			return p, errors.New("kiosk package must be allowlisted")
		}
	}
	if len(p.Restrictions) > len(restrictions) || len(p.Permissions) > 64 {
		return p, errors.New("policy rule limit exceeded")
	}
	sort.Slice(p.Restrictions, func(i, j int) bool { return p.Restrictions[i].Name < p.Restrictions[j].Name })
	for i, r := range p.Restrictions {
		if !slices.Contains(restrictions, r.Name) || i > 0 && p.Restrictions[i-1].Name == r.Name {
			return p, errors.New("unsupported or duplicate restriction")
		}
	}
	sort.Slice(p.Permissions, func(i, j int) bool {
		a, b := p.Permissions[i], p.Permissions[j]
		return a.PackageName+":"+a.Permission < b.PackageName+":"+b.Permission
	})
	for i, r := range p.Permissions {
		if !validPackage(r.PackageName) || !slices.Contains(p.RequiredPackages, r.PackageName) || !strings.HasPrefix(r.Permission, "android.permission.") || len(r.Permission) > 256 || !packagePattern.MatchString(r.Permission) || (r.State != "DEFAULT" && r.State != "GRANTED" && r.State != "DENIED") {
			return p, errors.New("valid required package, Android permission and DEFAULT/GRANTED/DENIED required")
		}
		if i > 0 && p.Permissions[i-1].PackageName == r.PackageName && p.Permissions[i-1].Permission == r.Permission {
			return p, errors.New("duplicate permission rule")
		}
	}
	if p.MaximumTimeToLockMillis != nil && (*p.MaximumTimeToLockMillis < 0 || *p.MaximumTimeToLockMillis > 86400000) {
		return p, errors.New("maximum time to lock must be 0–86400000 ms")
	}
	return p, nil
}

// PolicyExpectations defines canonical values shared with the Android observer.
// Required apps bind package/version/signers, so changing an APK invalidates an
// earlier policy observation even if the policy document itself is unchanged.
// Usage: values := ports.PolicyExpectations(policy, desiredApps).
func PolicyExpectations(p Policy, apps []DesiredApplication) map[string]string {
	values := map[string]string{"cleanup": "complete"}
	for _, name := range p.RequiredPackages {
		values["required:"+name] = "missing_assignment"
		for _, app := range apps {
			if app.Artifact.PackageName == name {
				certs := slices.Clone(app.Artifact.SigningCertificates)
				sort.Strings(certs)
				values["required:"+name] = fmt.Sprintf("%s@%d:%s", name, app.Artifact.VersionCode, strings.Join(certs, ","))
			}
		}
	}
	if p.CameraDisabled != nil {
		values["camera"] = strconv.FormatBool(*p.CameraDisabled)
	}
	if p.ScreenCaptureDisabled != nil {
		values["screen_capture"] = strconv.FormatBool(*p.ScreenCaptureDisabled)
	}
	if p.MaximumTimeToLockMillis != nil {
		values["maximum_time_to_lock"] = strconv.FormatInt(*p.MaximumTimeToLockMillis, 10)
	}
	for _, r := range p.Restrictions {
		values["restriction:"+r.Name] = strconv.FormatBool(r.Enabled)
	}
	for _, r := range p.Permissions {
		values["permission:"+r.PackageName+":"+r.Permission] = r.State
	}
	if p.LockTask != nil {
		// A fixed field order and sorted packages keep Go/Kotlin values identical.
		list := slices.Clone(p.LockTask.Packages)
		if list == nil {
			list = []string{}
		}
		sort.Strings(list)
		encoded, _ := json.Marshal(struct {
			Packages []string `json:"packages"`
			Kiosk    string   `json:"kiosk_package"`
		}{list, p.LockTask.KioskPackage})
		values["lock_task"] = string(encoded)
	}
	return values
}

// PolicyCapability identifies the agent capability required to implement one canonical rule ID.
// Usage: capability := ports.PolicyCapability("camera").
func PolicyCapability(key string) string {
	switch {
	case strings.HasPrefix(key, "required:"):
		return "install_apk"
	case strings.HasPrefix(key, "permission:"):
		return "policy.permissions"
	case strings.HasPrefix(key, "restriction:"):
		return "policy.restrictions"
	case key == "lock_task":
		return "policy.kiosk"
	case key == "camera":
		return "policy.camera"
	case key == "screen_capture":
		return "policy.screen_capture"
	case key == "maximum_time_to_lock":
		return "policy.maximum_time_to_lock"
	default:
		return "durable_sync"
	}
}
