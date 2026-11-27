//go:build windows

package ui

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

func TestNotLicensedMethodsMatchFrontendFixture(t *testing.T) {
	raw, err := os.ReadFile("../frontend/src/api/fixtures/notLicensedMethods.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	var got []string
	for name := range NotLicensedMethods(&Host{}) {
		got = append(got, name)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NotLicensedMethods = %v, frontend fixture = %v", got, want)
	}
}
