package routerconfig

import (
	"errors"
	"testing"
)

// AStudio keeps its own model settings; 端云协同 cannot remove the endpoint the
// user added there. Its Profile switch must therefore stop requests, while
// host-managed applications keep their existing behavior.
func TestManualHostAppSwitchGatesRequests(t *testing.T) {
	s := providerSetup(t)
	if _, err := s.SaveGroup(Group{ID: "edge", Name: "Edge", Routing: "smart_split", Members: []string{"local/spark-x2.6-4B"}}); err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}

	on, err := s.SaveApp(AppEdit{ID: "astudio", Name: "AStudio", CredentialMode: StoredCredential, Enabled: boolPtr(true), Models: slicePtr("group/edge")})
	if err != nil {
		t.Fatalf("SaveApp(astudio on): %v", err)
	}
	if target, err := s.ResolveApp("astudio", "Bearer wefto-local-placeholder", ""); err != nil || target.Profile.ID != "astudio" {
		t.Fatalf("switched-on AStudio must resolve, got target=%+v err=%v", target, err)
	}

	off, err := s.SaveApp(AppEdit{ID: "astudio", Revision: on.Revision, Name: "AStudio", CredentialMode: StoredCredential, Enabled: boolPtr(false), Models: slicePtr("group/edge")})
	if err != nil {
		t.Fatalf("SaveApp(astudio off): %v", err)
	}
	if len(off.Models) != 1 || off.Models[0] != "group/edge" {
		t.Fatalf("switching off must keep the selection for the next connection, got %v", off.Models)
	}
	if _, err := s.ResolveApp("astudio", "Bearer wefto-local-placeholder", ""); !errors.Is(err, ErrAppDisabled) {
		t.Fatalf("switched-off AStudio must be refused with ErrAppDisabled, got %v", err)
	}

	// A host-managed application keeps serving a configured but disabled Profile;
	// its host file, not the switch, decides whether requests arrive.
	if _, err := s.SaveApp(AppEdit{ID: "workbuddy", Name: "WorkBuddy", CredentialMode: StoredCredential, Enabled: boolPtr(false), Models: slicePtr("group/edge")}); err != nil {
		t.Fatalf("SaveApp(workbuddy): %v", err)
	}
	if _, err := s.ResolveApp("workbuddy", "", ""); err != nil {
		t.Fatalf("host-managed WorkBuddy must not be gated by the switch, got %v", err)
	}
}

func TestBuiltinAStudioIsListedWithItsProductName(t *testing.T) {
	s := providerSetup(t)
	apps, err := s.Apps()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range apps {
		if a.ID == "astudio" {
			if a.Name != "AStudio" || a.Path != "/app/astudio/v1" || a.CredentialMode != StoredCredential {
				t.Fatalf("unexpected built-in AStudio Profile: %+v", a)
			}
			if !ManualHostApp(a.ID) {
				t.Fatal("AStudio must be a manual-host application")
			}
			return
		}
	}
	t.Fatal("built-in AStudio Profile is missing")
}
