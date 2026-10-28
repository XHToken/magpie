package routerconfig

import (
	"errors"
	"reflect"
	"testing"
)

func TestActiveGroupExpansionPreservesPrivacyAcrossPaths(t *testing.T) {
	child := Group{ID: "private", Name: "Private", LocalRedaction: true, Members: []string{"local/spark-x2.6-4b:high", "cloud/model"}}
	for _, scenario := range []string{"nested", "duplicate-first", "excluded-group", "excluded-model", "disabled", "hidden"} {
		t.Run(scenario, func(t *testing.T) {
			sub := child
			root := Group{ID: "root", Members: []string{"group/private", "cloud/model"}}
			switch scenario {
			case "duplicate-first":
				root.Members = []string{"cloud/model", "group/private"}
			case "excluded-group":
				root.Off = []string{"group/private"}
			case "excluded-model":
				root.Off = []string{"local/spark-x2.6-4b"}
			case "disabled":
				sub.Disabled = true
			case "hidden":
				sub.Hidden = true
			}
			got, err := ResolveActiveGroupExpansion(root, []Group{root, sub})
			if err != nil {
				t.Fatal(err)
			}
			want := []GroupPrivacySource{{GroupID: "private", ModelID: "spark-x2.6-4b"}}
			switch scenario {
			case "excluded-group", "disabled", "hidden":
				want = nil
			case "excluded-model":
				want[0].ModelID = ""
			}
			if !reflect.DeepEqual(got.PrivacySources, want) {
				t.Fatalf("privacy=%+v want=%+v", got.PrivacySources, want)
			}
		})
	}
}

func TestActiveGroupExpansionRejectsIncompleteGraphs(t *testing.T) {
	root := Group{ID: "root", Members: []string{"group/missing", "cloud/model"}}
	if _, err := ResolveActiveGroupExpansion(root, []Group{root}); !errors.Is(err, ErrGroupExpansion) {
		t.Fatalf("missing reference: %v", err)
	}
	root.Members = []string{"group/root"}
	if _, err := ResolveActiveGroupExpansion(root, []Group{root}); !errors.Is(err, ErrGroupExpansion) {
		t.Fatalf("cycle: %v", err)
	}
}

func TestNormalizeGroupRetainsUnavailableRedactionIntent(t *testing.T) {
	g, err := NormalizeGroup(Group{ID: "private", Name: "Private", LocalRedaction: true, Members: []string{"cloud/model"}})
	if err != nil || !g.LocalRedaction {
		t.Fatalf("privacy intent cleared: %+v %v", g, err)
	}
}
