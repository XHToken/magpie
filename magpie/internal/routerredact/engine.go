package routerredact

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/cloudcodec"
	"github.com/yetone/magpie/internal/routerruntime"
)

const EngineVersion = "privacy-user-input-v1"
const EngineTrialVersion = "privacy-user-input-trial-v1"
const EngineMaxChunks = 64

// NewEngineSpecification binds the complete production detector, rather than
// borrowing acceptance from the legacy model-only extraction specification.
func NewEngineSpecification(binding ModelBinding, experimental bool) (*PrivacySpecification, error) {
	s, err := newPrivacySpecification(binding, experimental)
	if err != nil {
		return nil, err
	}
	s.unified = true
	labels := PrivacyLabels()
	labels["private_person"] = "NAME"
	for _, label := range []string{"IP", "MAC", "HOSTNAME"} {
		labels[strings.ToLower(label)] = label
	}
	s.engineLabels = labels
	s.enginePrompt = strings.ReplaceAll(PrivacyPrompt(), "模型自行识别，没有正则或词典兜底；", "规则与词典也会检查；你仍须独立完整识别每个字段；")
	s.enginePrompt = strings.ReplaceAll(s.enginePrompt, "所有有效标签含public_information均做可逆替换，不能白名单放行；", "敏感标签做可逆替换；public_information与job_title仅报告非敏感内容，不能覆盖敏感子实体；")
	s.enginePrompt += "\n补充标签：ip:IP地址；mac:MAC地址；hostname:内部主机名。每个输入field是一个完整扫描块；仍须返回所有field及精确块内坐标。"
	var schema map[string]any
	if json.Unmarshal([]byte(PrivacySchema()), &schema) != nil {
		return nil, fail("redaction_specification_failed")
	}
	enum := make([]string, 0, len(labels))
	for label := range labels {
		enum = append(enum, label)
	}
	sort.Strings(enum)
	schema["properties"].(map[string]any)["fields"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["findings"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["label"].(map[string]any)["enum"] = enum
	rawSchema, _ := json.Marshal(schema)
	s.engineSchema = string(rawSchema)
	builtins, _ := json.Marshal(defaultRuleDefinitions)
	policy, _ := json.Marshal(map[string]any{
		"hostPromptProjection": cloudcodec.UserPromptSourceVersion,
		"scope":                "user-input-v1", "source": "original-user-text-v1", "knownValues": "exact-longest-boundary-v1", "cache": "verified-user-prefix-v1", "session": "service-hmac-app-15min-instance-v1", "secondaryScope": "assistant-text-tool-output-string-arguments-v1", "literalTokens": "source-and-target-reserved-allowlisted-emitted-v1",
		"revision": EngineVersion, "experimental": experimental,
		"labels": labels, "pass": []string{"public_information", "job_title"},
		"rules":      string(defaultPolicyYAML) + string(rulesZhYAML),
		"dictionary": string(defaultDictionaryYAML), "builtins": string(builtins),
		"canonicalLabels": CanonicalLabelMap, "passLabels": AllowedPassThroughLabels,
		"chunkChars": DefaultLLMMaxChunkChars, "overlap": DefaultLLMChunkOverlap,
		"maxChunks": EngineMaxChunks, "maxFields": 8192, "maxBytes": 8 << 20,
		"maxEntities": maxEntities, "maxEntityBytes": maxEntityBytes,
		"maxBatchFields": PrivacyMaxBatchFields, "maxBatchRunes": PrivacyMaxBatchRunes,
		"outputTokens": PrivacyMaxOutputTokens, "budgetNs": routerruntime.DefaultRedactionBudget,
		"amount": "reversible", "review": false, "heuristicFiltering": false,
		"coordinates": "unique-exact-substring-valid-coordinates-v1",
		"merge":       "sensitive-union-prefer-fine-category-v1", "completion": "strict-fields-v1",
	})
	s.fingerprints.Prompt = privacyHash([]byte(s.enginePrompt))
	s.fingerprints.Schema = privacyHash(rawSchema)
	s.fingerprints.Config = privacyHash(policy)
	material, _ := json.Marshal([]string{s.enginePrompt, s.engineSchema})
	s.fingerprints.PromptSchema = privacyHash(material)
	version := EngineVersion
	if experimental {
		version = EngineTrialVersion
	}
	s.fingerprints.Suite = version + ":" + privacyHash([]byte(s.fingerprints.PromptSchema+":"+s.fingerprints.Config))
	s.versions = Versions{s.fingerprints.Model, s.fingerprints.PromptSchema, s.fingerprints.Config}
	return s, nil
}

func (s *PrivacySpecification) ModelID() string { return s.modelID }

func (s *PrivacySpecification) labels() map[string]string {
	if s.unified {
		return s.engineLabels
	}
	return PrivacyLabels()
}
func (s *PrivacySpecification) prompt() string {
	if s.unified {
		return s.enginePrompt
	}
	return PrivacyPrompt()
}
func (s *PrivacySpecification) schema() string {
	if s.unified {
		return s.engineSchema
	}
	return PrivacySchema()
}

// HybridExtractor is shared by every policy source. Rule/dictionary scanning
// covers whole fields, while strict local-model batches cover every chunk.
type HybridExtractor struct {
	runtime *RuntimeExtractor
	spec    *PrivacySpecification
}

func NewHybridExtractor(runtime RuntimeInvoker, spec *PrivacySpecification) *HybridExtractor {
	extractor := NewRuntimeExtractor(runtime, spec)
	if invoker, ok := runtime.(*routerruntime.ExperimentalRedactionInvoker); ok {
		extractor = NewExperimentalRuntimeExtractor(invoker, spec)
	}
	return &HybridExtractor{runtime: extractor, spec: spec}
}

func (e *HybridExtractor) Available(ctx context.Context, versions Versions) error {
	if e == nil || e.spec == nil || !e.spec.unified {
		return fail("redaction_unavailable")
	}
	return e.runtime.Available(ctx, versions)
}

func (e *HybridExtractor) Extract(ctx context.Context, in Extraction) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, routerruntime.DefaultRedactionBudget)
	defer cancel()
	if err := e.Available(ctx, in.Versions); err != nil {
		return Result{}, err
	}
	if len(in.Texts) > 8192 {
		return Result{}, fail("redaction_input_limit")
	}
	type part struct {
		field, offset int
		text          string
	}
	var parts []part
	spans := make([][]Entity, len(in.Texts))
	textBytes := 0
	for field, text := range in.Texts {
		textBytes += len(text)
		if textBytes > 8<<20 || !utf8.ValidString(text) {
			return Result{}, fail("redaction_input_limit")
		}
		if err := contextError(ctx); err != nil {
			return Result{}, err
		}
		rules, err := DefaultRuleDetector().DetectContext(ctx, text)
		if err != nil {
			return Result{}, err
		}
		rules = append(rules, DefaultDictionaryDetector().Detect(text)...)
		for _, span := range rules {
			category := span.CanonicalLabel
			if category == "" {
				category = CanonicalizeLabel(span.Label)
			}
			if AllowedPassThroughLabels[category] {
				continue
			}
			spans[field] = append(spans[field], Entity{Field: field, Start: span.Start, End: span.End, Text: span.Value, Category: category})
		}
		for _, chunk := range ChunkText(text, DefaultLLMMaxChunkChars, DefaultLLMChunkOverlap) {
			parts = append(parts, part{field, chunk.ByteOffset, chunk.Text})
			if len(parts) > EngineMaxChunks {
				return Result{}, fail("redaction_input_limit")
			}
		}
	}
	// A batch never crosses the strict parser's complete-field limits. Every
	// invocation inherits the caller's one preparation deadline.
	calls := 0
	for start := 0; start < len(parts); {
		end, runes := start, 0
		for end < len(parts) && end-start < PrivacyMaxBatchFields {
			n := len([]rune(parts[end].text))
			if end > start && runes+n > PrivacyMaxBatchRunes {
				break
			}
			runes += n
			end++
		}
		batch := Extraction{Versions: in.Versions}
		for _, p := range parts[start:end] {
			batch.Texts = append(batch.Texts, p.text)
		}
		result, err := e.runtime.Extract(ctx, batch)
		if err != nil {
			return Result{}, err
		}
		calls += result.LocalInferenceCalls
		for _, entity := range result.Entities {
			if entity.Category == "PUBLICINFORMATION" || entity.Category == "JOBTITLE" {
				continue
			}
			p := parts[start+entity.Field]
			entity.Field = p.field
			entity.Start += p.offset
			entity.End += p.offset
			spans[p.field] = append(spans[p.field], entity)
		}
		start = end
	}
	var entities []Entity
	for field, found := range spans {
		sort.Slice(found, func(i, j int) bool {
			if found[i].Start != found[j].Start {
				return found[i].Start < found[j].Start
			}
			if found[i].End != found[j].End {
				return found[i].End > found[j].End
			}
			return found[i].Category < found[j].Category
		})
		for _, entity := range found {
			// Merge overlapping sensitive spans into their union. Never discard
			// the uncovered part of a valid credential in favor of another label.
			if len(entities) > 0 {
				last := &entities[len(entities)-1]
				if last.Field == field && entity.Start < last.End {
					// Prefer the model's fine category when a rule reports the
					// identical span under its broader canonical category.
					if entity.Start == last.Start && entity.End == last.End && last.Category == CanonicalizeLabel(entity.Category) && entity.Category != last.Category {
						last.Category = entity.Category
					}
					if entity.End > last.End {
						last.End = entity.End
						last.Text = in.Texts[field][last.Start:last.End]
					}
					continue
				}
			}
			entities = append(entities, entity)
		}
	}
	if err := e.Available(ctx, in.Versions); err != nil {
		return Result{}, err
	}
	validated, err := validateEntities(in.Texts, Result{Complete: true, Entities: entities})
	return Result{Complete: err == nil, Entities: validated, LocalInferenceCalls: calls, Chunks: len(parts)}, err
}
