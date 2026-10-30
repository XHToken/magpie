package routerconfig

import (
	"context"
	"testing"
)

func TestRecognitionScopeSurvivesTargetEditsButNotRegistrationRevocation(t *testing.T) {
	s := providerSetup(t)
	provider, err := s.SaveCloudProvider(providerDraft())
	if err != nil {
		t.Fatal(err)
	}
	edit := appDraft("scope-editor", provider.ID)
	saved, err := s.SaveApp(edit)
	if err != nil {
		t.Fatal(err)
	}
	original := saved.RecognitionScope()
	edit.Revision = saved.Revision
	edit.Cloud.Model = "another-model"
	enabled := true
	edit.Enabled = &enabled
	saved, err = s.SaveApp(edit)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RecognitionScope() != original {
		t.Fatal("ordinary target edit invalidated recognition")
	}
	admitted := 0
	if err = s.ConfirmRecognitionScopeContext(context.Background(), saved.ID, original, func() error { admitted++; return nil }); err != nil || admitted != 1 {
		t.Fatal("valid authority failed", err)
	}
	if err = s.DeleteApp(saved.ID, saved.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmRecognitionScopeContext(context.Background(), saved.ID, original, func() error { admitted++; return nil }); err == nil || admitted != 1 {
		t.Fatal("revoked application admitted a new attempt")
	}
	edit.Revision = 0
	saved, err = s.SaveApp(edit)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RecognitionScope() == original {
		t.Fatal("recreated application inherited revoked scope")
	}
}
