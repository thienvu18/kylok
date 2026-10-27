// SPDX-License-Identifier: LGPL-3.0-only
package ports

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"unicode/utf8"

	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"google.golang.org/protobuf/proto"
)

// ManagedConfiguration owns the complete restrictions bundle for a required managed app.
// Absence from a replacement policy releases only the bundle journaled by Kylok.
type ManagedConfiguration struct {
	PackageName string              `json:"package_name"`
	Values      ConfigurationBundle `json:"values"`
}

// ConfigurationBundle is a bounded typed Android Bundle. Normalization sorts keys; array order remains significant.
type ConfigurationBundle struct {
	Entries []ConfigurationEntry `json:"entries"`
}

// ConfigurationEntry assigns one unique application-defined key to exactly one typed value.
type ConfigurationEntry struct {
	Key   string             `json:"key"`
	Value ConfigurationValue `json:"value"`
}

// ConfigurationValue selects exactly one nonnil pointer. Empty strings/arrays/bundles are supported values.
// Boolean/int/string pointers preserve explicit zero values and prevent type inference from generic JSON.
type ConfigurationValue struct {
	Boolean *bool                  `json:"boolean,omitempty"`
	Integer *int32                 `json:"integer,omitempty"`
	String  *string                `json:"string,omitempty"`
	Strings *[]string              `json:"strings,omitempty"`
	Bundle  *ConfigurationBundle   `json:"bundle,omitempty"`
	Bundles *[]ConfigurationBundle `json:"bundles,omitempty"`
}

// MaximumPolicyResults covers 64 required apps, permissions and configurations, four restrictions, three scalars, kiosk and cleanup.
const MaximumPolicyResults = 201

const maximumConfigurationBytes = 65536
const maximumConfigurationEntries = 256
const maximumConfigurationDepth = 4

var configurationKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)

// Validate recursive value types/limits, then canonicalize unordered keys for idempotency and cross-platform digests.
func normalizeBundle(bundle *ConfigurationBundle, depth int, nodes *int) error {
	if bundle.Entries == nil {
		bundle.Entries = []ConfigurationEntry{}
	}
	if depth > maximumConfigurationDepth || len(bundle.Entries) > 64 {
		return errors.New("configuration nesting/key limit exceeded")
	}
	sort.Slice(bundle.Entries, func(i, j int) bool { return bundle.Entries[i].Key < bundle.Entries[j].Key })
	for i := range bundle.Entries {
		entry := &bundle.Entries[i]
		*nodes += 1
		if *nodes > maximumConfigurationEntries || !configurationKey.MatchString(entry.Key) || i > 0 && bundle.Entries[i-1].Key == entry.Key {
			return errors.New("invalid/duplicate configuration key or entry limit exceeded")
		}
		value := &entry.Value
		types := 0
		if value.Boolean != nil {
			types++
		}
		if value.Integer != nil {
			types++
		}
		validString := func(value string) bool { return len(value) <= 4096 && utf8.ValidString(value) }
		if value.String != nil {
			types++
			if !validString(*value.String) {
				return errors.New("invalid/oversize configuration string")
			}
		}
		if value.Strings != nil {
			if *value.Strings == nil {
				*value.Strings = []string{}
			}
			types++
			if len(*value.Strings) > 64 {
				return errors.New("configuration array limit exceeded")
			}
			for _, item := range *value.Strings {
				if !validString(item) {
					return errors.New("invalid configuration array string")
				}
			}
		}
		if value.Bundle != nil {
			types++
			if err := normalizeBundle(value.Bundle, depth+1, nodes); err != nil {
				return err
			}
		}
		if value.Bundles != nil {
			if *value.Bundles == nil {
				*value.Bundles = []ConfigurationBundle{}
			}
			types++
			if len(*value.Bundles) > 64 {
				return errors.New("configuration bundle-array limit exceeded")
			}
			for i := range *value.Bundles {
				if err := normalizeBundle(&(*value.Bundles)[i], depth+1, nodes); err != nil {
					return err
				}
			}
		}
		if types != 1 {
			return errors.New("configuration value must select exactly one type")
		}
	}
	return nil
}

// Normalize configuration documents within the policy's verified required-app boundary.
func normalizeConfigurations(policy *Policy) error {
	if len(policy.ManagedConfigurations) > 64 {
		return errors.New("at most 64 managed configurations")
	}
	sort.Slice(policy.ManagedConfigurations, func(i, j int) bool {
		return policy.ManagedConfigurations[i].PackageName < policy.ManagedConfigurations[j].PackageName
	})
	nodes := 0
	for i := range policy.ManagedConfigurations {
		config := &policy.ManagedConfigurations[i]
		if !packagePattern.MatchString(config.PackageName) || len(config.PackageName) > 256 || i > 0 && policy.ManagedConfigurations[i-1].PackageName == config.PackageName {
			return errors.New("invalid/duplicate configuration package")
		}
		assigned := false
		for _, name := range policy.RequiredPackages {
			if name == config.PackageName {
				assigned = true
			}
		}
		if !assigned {
			return errors.New("configuration target must be a required package")
		}
		if err := normalizeBundle(&config.Values, 1, &nodes); err != nil {
			return err
		}
	}
	data, err := json.Marshal(policy.ManagedConfigurations)
	if err != nil {
		return err
	}
	if len(data) > maximumConfigurationBytes {
		return errors.New("managed configuration payload exceeds 64 KiB")
	}
	return nil
}

// ConfigurationMessage preserves explicit scalar values and typed arrays at the wire boundary.
// The input must be normalized with NormalizePolicy before hashing or transmission.
// Usage: wire := ports.ConfigurationMessage(normalized.ManagedConfigurations[0].Values).
func ConfigurationMessage(bundle ConfigurationBundle) *v1.ConfigurationBundle {
	result := &v1.ConfigurationBundle{}
	for _, entry := range bundle.Entries {
		value := &v1.ConfigurationValue{}
		switch {
		case entry.Value.Boolean != nil:
			value.Value = &v1.ConfigurationValue_BooleanValue{BooleanValue: *entry.Value.Boolean}
		case entry.Value.Integer != nil:
			value.Value = &v1.ConfigurationValue_IntegerValue{IntegerValue: *entry.Value.Integer}
		case entry.Value.String != nil:
			value.Value = &v1.ConfigurationValue_StringValue{StringValue: *entry.Value.String}
		case entry.Value.Strings != nil:
			value.Value = &v1.ConfigurationValue_StringsValue{StringsValue: &v1.ConfigurationStrings{Values: *entry.Value.Strings}}
		case entry.Value.Bundle != nil:
			value.Value = &v1.ConfigurationValue_BundleValue{BundleValue: ConfigurationMessage(*entry.Value.Bundle)}
		case entry.Value.Bundles != nil:
			list := &v1.ConfigurationBundles{}
			for _, item := range *entry.Value.Bundles {
				list.Values = append(list.Values, ConfigurationMessage(item))
			}
			value.Value = &v1.ConfigurationValue_BundlesValue{BundlesValue: list}
		}
		result.Entries = append(result.Entries, &v1.ConfigurationEntry{Key: entry.Key, Value: value})
	}
	return result
}

// ConfigurationDigest reports only a digest of normalized typed content, never configuration values.
// A matching DPC readback is separate from acknowledgement that an application applied the configuration.
// Usage: expected := ports.ConfigurationDigest(normalized.ManagedConfigurations[0].Values).
func ConfigurationDigest(bundle ConfigurationBundle) string {
	data, _ := proto.MarshalOptions{Deterministic: true}.Marshal(ConfigurationMessage(bundle))
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
