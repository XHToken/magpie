package routermanifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFixedCatalogHasOnlyVerifiedExecutionArtifacts(t *testing.T) {
	c := Fixed()
	if c.SchemaVersion != 1 || len(c.Models) != 2 {
		t.Fatal(c)
	}
	for _, retired := range []string{"spark-x2.5-1.7B", "spark-x2.5-4B"} {
		if _, ok := c.Model(retired); ok {
			t.Fatalf("retired model still has an installation plan: %s", retired)
		}
	}
	if c.Models[0].Quantization != "Q4_K_M" || c.Models[1].Quantization != "Q8_0" || c.Models[0].Artifact.ID == c.Models[1].Artifact.ID {
		t.Fatal("quantized weights must have independent download identities")
	}
	for _, m := range c.Models {
		if err := m.Artifact.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := m.Template.Validate(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(m.Artifact.URL, m.Revision) || !strings.Contains(m.Template.URL, m.TemplateRevision) || (m.Quantization != "Q4_K_M" && m.Quantization != "Q8_0") || m.Purpose != "public_execution_and_redaction_validation_only" || m.InstallationBytes <= m.Artifact.Size || m.License == "" || m.ContextTokens > m.NativeContextTokens {
			t.Fatal(m)
		}
		for _, r := range c.Runtimes {
			if err := r.Artifact.Validate(); err != nil {
				t.Fatal(err)
			}
			if r.Version != "b10828" || r.Commit != "3ad1ba7336986d98592d3e28cafd1a406715351f" {
				t.Fatal(r)
			}
			before := m.Fingerprint(r)
			changed := m
			changed.Revision = "another"
			if changed.Fingerprint(r) == before {
				t.Fatal("revision not bound to capability")
			}
			changed = m
			changed.Template.SHA256 = strings.Repeat("0", 64)
			if changed.Fingerprint(r) == before {
				t.Fatal("template not bound to capability")
			}
			r.Version = "another"
			if m.Fingerprint(r) == before {
				t.Fatal("runtime not bound to capability")
			}
		}
	}
	first := c.Models[0].Artifact.SHA256
	c.Models[0].Artifact.SHA256 = "changed"
	if Fixed().Models[0].Artifact.SHA256 != first {
		t.Fatal("mutable shared catalog")
	}
}

func TestRedactionTrialSupportRejectsUnlistedOrUndeclaredModels(t *testing.T) {
	for _, model := range Fixed().Models {
		if !model.SupportsRedactionTrial() {
			t.Fatalf("fixed model %s cannot enter redaction trial", model.ID)
		}
		model.RedactionTrial = false
		if model.SupportsRedactionTrial() {
			t.Fatal("model without trial support was authorized")
		}
		model.RedactionTrial, model.ID = true, "unlisted-model"
		if model.SupportsRedactionTrial() {
			t.Fatal("unlisted model inherited trial support")
		}
	}
}

func TestFixedCatalogAdvertisesBothX26RedactionTrials(t *testing.T) {
	raw, err := json.Marshal(Fixed())
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Models []struct {
			ID             string `json:"id"`
			Purpose        string `json:"purpose"`
			RedactionTrial bool   `json:"redactionTrial"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"spark-x2.6-4B", "spark-x2.6-4B-Q8_0"} {
		found := false
		for _, model := range catalog.Models {
			if model.ID != id {
				continue
			}
			found = true
			if !model.RedactionTrial || model.Purpose != "public_execution_and_redaction_validation_only" {
				t.Fatalf("%s does not advertise experimental redaction: %+v", id, model)
			}
		}
		if !found {
			t.Fatalf("missing fixed model %s", id)
		}
	}
}

func TestUnknownOrUnsafeArtifactCannotInstall(t *testing.T) {
	good := Fixed().Models[0].Artifact
	for _, edit := range []func(*Artifact){func(a *Artifact) { a.Size = 0 }, func(a *Artifact) { a.SHA256 = "unknown" }, func(a *Artifact) { a.URL = "" }, func(a *Artifact) { a.URL = "file:///private" }, func(a *Artifact) { a.FileName = "../model" }, func(a *Artifact) { a.Format = "unknown" }} {
		a := good
		edit(&a)
		if a.Validate() == nil {
			t.Fatal(a)
		}
	}
}
