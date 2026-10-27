// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go"
	"github.com/thienvu18/kylok/go/internal/testapk"
	"github.com/thienvu18/kylok/go/ports"
)

func TestTypedManagedConfigurationNormalizationAndBounds(t *testing.T) {
	b := true
	n := int32(-7)
	label := "fixture"
	words := []string{"a", "b"}
	empty := []string{}
	bundles := []ports.ConfigurationBundle{{Entries: []ports.ConfigurationEntry{{Key: "nested", Value: ports.ConfigurationValue{Boolean: &b}}}}}
	config := ports.ManagedConfiguration{PackageName: "io.test.app", Values: ports.ConfigurationBundle{Entries: []ports.ConfigurationEntry{
		{Key: "strings", Value: ports.ConfigurationValue{Strings: &words}}, {Key: "int", Value: ports.ConfigurationValue{Integer: &n}}, {Key: "bool", Value: ports.ConfigurationValue{Boolean: &b}},
		{Key: "string", Value: ports.ConfigurationValue{String: &label}}, {Key: "empty", Value: ports.ConfigurationValue{Strings: &empty}},
		{Key: "bundle", Value: ports.ConfigurationValue{Bundle: &bundles[0]}}, {Key: "bundles", Value: ports.ConfigurationValue{Bundles: &bundles}},
	}}}
	p, err := ports.NormalizePolicy(ports.Policy{RequiredPackages: []string{"io.test.app"}, ManagedConfigurations: []ports.ManagedConfiguration{config}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(p)
	var duplicate ports.Policy
	if err = json.Unmarshal(encoded, &duplicate); err != nil {
		t.Fatal(err)
	}
	normalized, err := ports.NormalizePolicy(duplicate)
	if err != nil || !reflect.DeepEqual(p, normalized) {
		t.Fatal(normalized, err)
	}
	digest := ports.ConfigurationDigest(p.ManagedConfigurations[0].Values)
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		t.Fatal(digest)
	}
	if ports.PolicyExpectations(p, nil)["configuration:io.test.app"] != digest || ports.PolicyCapability("configuration:io.test.app") != "policy.managed_configuration" {
		t.Fatal("missing expectation/capability")
	}
	for _, invalid := range []ports.Policy{
		{ManagedConfigurations: []ports.ManagedConfiguration{config}},
		{RequiredPackages: []string{"io.test.app"}, ManagedConfigurations: []ports.ManagedConfiguration{config, config}},
		{RequiredPackages: []string{"io.test.app"}, ManagedConfigurations: []ports.ManagedConfiguration{{PackageName: "io.test.app", Values: ports.ConfigurationBundle{Entries: []ports.ConfigurationEntry{{Key: "key", Value: ports.ConfigurationValue{}}}}}}},
		{RequiredPackages: []string{"io.test.app"}, ManagedConfigurations: []ports.ManagedConfiguration{{PackageName: "io.test.app", Values: ports.ConfigurationBundle{Entries: []ports.ConfigurationEntry{{Key: "key", Value: ports.ConfigurationValue{String: &label, Boolean: &b}}}}}}},
	} {
		if _, err = ports.NormalizePolicy(invalid); err == nil {
			t.Fatal("accepted invalid configuration", invalid)
		}
	}
	tooLong := strings.Repeat("x", 4097)
	invalid := config
	invalid.Values = ports.ConfigurationBundle{Entries: []ports.ConfigurationEntry{{Key: "key", Value: ports.ConfigurationValue{String: &tooLong}}}}
	if _, err = ports.NormalizePolicy(ports.Policy{RequiredPackages: []string{"io.test.app"}, ManagedConfigurations: []ports.ManagedConfiguration{invalid}}); err == nil {
		t.Fatal("oversize value accepted")
	}
	recursive := ports.ConfigurationBundle{}
	for range 5 {
		child := recursive
		recursive = ports.ConfigurationBundle{Entries: []ports.ConfigurationEntry{{Key: "nested", Value: ports.ConfigurationValue{Bundle: &child}}}}
	}
	if _, err = ports.NormalizePolicy(ports.Policy{RequiredPackages: []string{"io.test.app"}, ManagedConfigurations: []ports.ManagedConfiguration{{PackageName: "io.test.app", Values: recursive}}}); err == nil {
		t.Fatal("deep configuration accepted")
	}
}

func TestManagedConfigurationWireReadbackAndMissingAssignment(t *testing.T) {
	sdk, _ := setup(t, nil)
	client, _ := start(t, sdk)
	id, credential := enrolled(t, sdk, client, "scope")
	value := "app title"
	configuration := kylok.ManagedConfiguration{PackageName: "io.kylok.unit", Values: kylok.ConfigurationBundle{Entries: []kylok.ConfigurationEntry{{Key: "display_name", Value: kylok.ConfigurationValue{String: &value}}}}}
	apk := testapk.Make(t, testapk.Manifest)
	artifact, err := sdk.RegisterApplication(ctx, "scope", apk)
	if err != nil {
		t.Fatal(err)
	}
	configuration.PackageName = artifact.PackageName
	policy := kylok.Policy{RequiredPackages: []string{artifact.PackageName}, ManagedConfigurations: []kylok.ManagedConfiguration{configuration}}
	if _, err = sdk.SetPolicy(ctx, "scope", id, "before-assignment", policy); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = sdk.AssignApplication(ctx, "scope", id, artifact.SHA256, "assign", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = sdk.SetPolicy(ctx, "scope", id, "configured", policy); err != nil {
		t.Fatal(err)
	}
	snapshot := exchange(t, client, id, credential, 1, 0)
	if len(snapshot.DesiredPolicy.Policy.ManagedConfigurations) != 1 || snapshot.DesiredPolicy.Policy.ManagedConfigurations[0].Values.Entries[0].Value.GetStringValue() != value {
		t.Fatal(snapshot)
	}
	status, err := sdk.GetPolicy(ctx, "scope", id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, result := range status.Compliance {
		if result.RuleID == "configuration:"+artifact.PackageName {
			found = true
			if result.State != "UNSUPPORTED" {
				t.Fatal("missing capability misreported", result)
			}
		}
	}
	if !found {
		t.Fatal(status)
	}
}
