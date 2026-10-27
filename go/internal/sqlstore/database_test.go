// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import "testing"

func TestStaticSQLBindingPreservesLiteralsAndQualifiesTables(t *testing.T) {
	d := &database{dialect: "postgres", schema: "host_owned"}
	query := `SELECT 'devices ? isn''t a parameter',"?",d.id FROM devices d JOIN desired_apps w ON w.device_id=d.id WHERE d.scope_id=? AND (?='' OR d.state=?)`
	expected := `SELECT 'devices ? isn''t a parameter',"?",d.id FROM "host_owned".devices d JOIN "host_owned".desired_apps w ON w.device_id=d.id WHERE d.scope_id=$1 AND ($2='' OR d.state=$3)`
	if got := d.bind(query); got != expected {
		t.Fatal(got, expected)
	}
	sqlite := &database{dialect: "sqlite"}
	if sqlite.bind(query) != query {
		t.Fatal("SQLite SQL changed")
	}
}
