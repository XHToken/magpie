package routerconfig

import (
	"encoding/json"
	"testing"
)

func TestAppModelAliasesStayWithinWorkBuddySelection(t *testing.T) {
	s := providerSetup(t)
	models := []string{"group/coding", "local/spark-x2.6-4b"}
	edit := AppEdit{ID: "workbuddy", Name: "WorkBuddy", Models: &models, CredentialMode: StoredCredential,
		ModelAliases: map[string]string{"编程": models[0], "Spark": models[1]}}
	p, err := s.SaveApp(edit)
	if err != nil {
		t.Fatal(err)
	}
	edit.Revision = p.Revision
	for _, bad := range []map[string]string{
		{"unknown": "group/unbound"}, {"": models[0]}, {" leading": models[0]},
		{"line\nbreak": models[0]}, {"custom-local:model": models[0]}, {models[0]: models[1]},
	} {
		edit.ModelAliases = bad
		if _, err := s.SaveApp(edit); err == nil {
			t.Fatal("invalid host alias was accepted")
		}
	}
	edit.ID, edit.Name, edit.Revision = "loomy", "Loomy", 0
	edit.ModelAliases = map[string]string{"编程": models[0]}
	if _, err := s.SaveApp(edit); err == nil {
		t.Fatal("another app accepted WorkBuddy model aliases")
	}
	// The field is server-owned even when a client submits a matching JSON key.
	var input AppEdit
	if err := json.Unmarshal([]byte(`{"id":"workbuddy","modelAliases":{"evil":"group/other"}}`), &input); err != nil || input.ModelAliases != nil {
		t.Fatal("HTTP input supplied server-owned aliases")
	}
	if err := s.DeleteApp(p.ID, p.Revision); err != nil {
		t.Fatal(err)
	}
	if reset := findApp(t, s, "workbuddy"); len(reset.ModelAliases) != 0 {
		t.Fatal("clearing the preset retained routing aliases")
	}
}
