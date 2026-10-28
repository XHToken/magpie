package routerconfig

import (
	"context"
	"errors"
	"testing"
)

func TestProviderCatalogueCacheFollowsConnectionIdentity(t *testing.T) {
	for _, change := range []string{"base", "protocol", "key", "headers", "proxy", "name", "unchanged-key"} {
		t.Run(change, func(t *testing.T) {
			s := providerSetup(t)
			in := providerDraft()
			p, err := s.SaveCloudProvider(in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SaveProviderDiscoveredModels(p.ID, []string{"old-model"}); err != nil {
				t.Fatal(err)
			}
			in.ID, in.Revision = p.ID, p.Revision
			switch change {
			case "base":
				in.BaseURL = "https://other.invalid/v1"
			case "protocol":
				in.Protocol = "anthropic"
			case "key":
				key := "replacement-key"
				in.APIKey = &key
			case "headers":
				headers := map[string]string{"X-Tenant": "other-tenant"}
				in.Headers = &headers
			case "proxy":
				in.ProxyMode = "direct"
			case "name":
				in.Name = "Renamed"
			}
			p, err = s.SaveCloudProvider(in)
			if err != nil {
				t.Fatal(err)
			}
			wantCache := change == "name" || change == "unchanged-key"
			if (len(p.DiscoveredModels) > 0) != wantCache {
				t.Fatalf("cache survived incorrect connection identity: %s: %v", change, p.DiscoveredModels)
			}
		})
	}
}

func TestProviderDiscoveryStatusAndLateResponse(t *testing.T) {
	s := providerSetup(t)
	in := providerDraft()
	p, err := s.SaveCloudProvider(in)
	if err != nil {
		t.Fatal(err)
	}
	in.ID, in.Revision = p.ID, p.Revision
	expected, err := s.ResolveCloudProvider(in)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = s.SaveProviderModelDiscoveryContext(ctx, expected, []string{"listed-model"}, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveProviderModelDiscoveryContext(ctx, expected, nil, "model_list_auth_failed"); err != nil {
		t.Fatal(err)
	}
	list, err := s.CloudProviders()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ModelListError != "model_list_auth_failed" || len(list[0].DiscoveredModels) != 1 {
		t.Fatalf("failed refresh lost cache/provenance: %+v", list[0])
	}
	in.BaseURL = "https://new-target.invalid/v1"
	if _, err = s.SaveCloudProvider(in); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveProviderModelDiscoveryContext(ctx, expected, []string{"late-old-model"}, ""); !errors.Is(err, ErrProviderConflict) {
		t.Fatalf("late response accepted: %v", err)
	}
	list, err = s.CloudProviders()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ModelListError != "" || len(list[0].DiscoveredModels) != 0 {
		t.Fatalf("new connection retained old discovery: %+v", list[0])
	}
}
