package routerconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestProviderRejectsOptionsThatCloudExecutionCannotDispatch(t *testing.T) {
	s := providerSetup(t)
	saved, err := s.SaveCloudProvider(providerDraft())
	if err != nil {
		t.Fatal(err)
	}
	config, _ := os.ReadFile(s.layout.ConfigFile())
	credentials, _ := os.ReadFile(s.layout.CredentialsFile())
	type editCase struct {
		name string
		edit func(*ProviderEdit)
	}
	var cases []editCase
	for _, name := range []string{"Content-Type", "accept-encoding"} {
		cases = append(cases, editCase{"header/" + name, func(in *ProviderEdit) {
			headers := map[string]string{name: "invalid-override"}
			in.Headers = &headers
		}})
	}
	for _, name := range []string{"models", "MODEL", "tool_choice", "functions", "function_call", "conversation", "background", "store", "modalities", "audio", "image", "images", "video", "web_search_options", "web_search", "plugins", "mcp_servers", "prompt", "tool_resources", "data_sources", "attachments", "file_ids", "provider", "route", "fallback", "fallbacks", "routing", "!metadata"} {
		cases = append(cases, editCase{"body/" + name, func(in *ProviderEdit) {
			body := map[string]json.RawMessage{name: json.RawMessage(`null`)}
			in.ExtraBody = &body
		}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := providerDraft()
			in.ID, in.Revision = saved.ID, saved.Revision
			tc.edit(&in)
			for _, operation := range []string{"resolve", "save"} {
				var err error
				if operation == "resolve" {
					_, err = s.ResolveCloudProvider(in)
				} else {
					_, err = s.SaveCloudProvider(in)
				}
				var validation *ProviderValidation
				if !errors.As(err, &validation) {
					t.Fatalf("%s accepted an option that every cloud request rejects: %v", operation, err)
				}
			}
			afterConfig, _ := os.ReadFile(s.layout.ConfigFile())
			afterCredentials, _ := os.ReadFile(s.layout.CredentialsFile())
			if !bytes.Equal(config, afterConfig) || !bytes.Equal(credentials, afterCredentials) {
				t.Fatal("invalid provider options changed the saved configuration or credentials")
			}
		})
	}
}
