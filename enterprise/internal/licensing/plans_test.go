package licensing

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcegraph/sourcegraph/enterprise/internal/license"
)

const testPlan Plan = "test"

func init() {
	allPlans = append(allPlans, testPlan)
}

func TestPlan_isKnown(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		if got, want := Plan("x").isKnown(), false; got != want {
			t.Error()
		}
	})
	t.Run("known", func(t *testing.T) {
		if got, want := testPlan.isKnown(), true; got != want {
			t.Error()
		}
	})
}

func TestInfo_Plan(t *testing.T) {
	tests := []struct {
		tags []string
		want Plan
	}{
		{tags: []string{"foo", testPlan.tag()}, want: testPlan},
		{tags: []string{"foo", testPlan.tag(), Plan("xyz").tag()}, want: testPlan},
		{tags: []string{"foo", Plan("xyz").tag(), testPlan.tag()}, want: testPlan},
		{tags: []string{"plan:old-starter-0"}, want: oldEnterpriseStarter},
		{tags: []string{"plan:old-enterprise-0"}, want: oldEnterprise},
		{tags: []string{"plan:team-0"}, want: team},
		{tags: []string{"plan:enterprise-0"}, want: enterprise0},
		{tags: []string{"plan:enterprise-1"}, want: enterprise1},
		{tags: []string{"plan:business-0"}, want: business0},
		{tags: []string{"starter"}, want: oldEnterpriseStarter},
		{tags: []string{"foo"}, want: oldEnterprise},
		{tags: []string{""}, want: oldEnterprise},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("tags: %v", test.tags), func(t *testing.T) {
			got := (&Info{Info: license.Info{Tags: test.tags}}).Plan()
			if got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestInfo_hasUnknownPlan(t *testing.T) {
	tests := []struct {
		tags    []string
		wantErr string
	}{
		{tags: []string{""}},
		{tags: []string{"foo"}},
		{tags: []string{"foo", oldEnterpriseStarter.tag()}},
		{tags: []string{"foo", oldEnterprise.tag()}},
		{tags: []string{"foo", team.tag()}},
		{tags: []string{"foo", enterprise0.tag()}},
		{tags: []string{"starter"}},

		{tags: []string{"foo", "plan:xyz"}, wantErr: `The license has an unrecognizable plan in tag "plan:xyz", please contact Sourcegraph support.`},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("tags: %v", test.tags), func(t *testing.T) {
			var gotErr string
			err := (&Info{Info: license.Info{Tags: test.tags}}).hasUnknownPlan()
			if err != nil {
				gotErr = err.Error()
			}

			if diff := cmp.Diff(test.wantErr, gotErr); diff != "" {
				t.Fatalf("Mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
