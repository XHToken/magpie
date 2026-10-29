package routerhost

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/product"
	"github.com/yetone/magpie/internal/steady"
)

const loomySidecarFile = "loomy-config.json"

func loomyExtraManagedModelPath(path []string, bok, aok, cok bool) bool {
	return len(path) == 5 && path[0] == "provider" && product.IsProvider(path[1]) && path[2] == "models" && !bok && !aok && cok
}

func loomyModelsCatalogCleared(doc map[string]any) bool {
	v, ok := lookup(doc, []string{"provider", providerID(doc, "provider"), "models"})
	if !ok {
		return true
	}
	m, ok := v.(map[string]any)
	return ok && len(m) == 0
}

func loomyReconnectableConflict(path []string, aok, cok bool, current any, _ map[string]any) bool {
	if len(path) == 3 && path[0] == "provider" && product.IsProvider(path[1]) && path[2] == "name" {
		return aok && product.IsProviderName(current)
	}
	if len(path) == 4 && path[0] == "provider" && product.IsProvider(path[1]) && path[2] == "options" && path[3] == "apiKey" {
		return aok && product.IsPlaceholder(current)
	}
	if len(path) == 1 && path[0] == "model" {
		return loomyRouterSelection(current)
	}
	if len(path) == 1 && path[0] == "small_model" {
		return loomyRouterSelection(current) || loomyPreservedSelector(current)
	}
	if len(path) == 5 && path[0] == "provider" && product.IsProvider(path[1]) && path[2] == "models" {
		if !cok {
			return true
		}
		switch path[4] {
		case "name":
			s, ok := current.(string)
			return ok && strings.TrimSpace(s) != ""
		case "tool_call":
			b, ok := current.(bool)
			return !aok && ok && b
		}
	}
	return false
}

// refreshLoomyRuntime synchronizes Loomy's sidecar flags when present and stops
// any stale embedded OpenCode server process so Loomy reloads opencode.json on
// the next session or message request.
func refreshLoomyRuntime(root string, files []snapshot) {
	var opencodeAfter []byte
	changed := false
	for _, f := range files {
		if f.Name == "opencode.json" {
			opencodeAfter = f.After
			if !equal(f.Before, f.After) || !f.Existed {
				changed = true
			}
		}
	}
	if syncLoomySidecar(root, opencodeAfter) {
		changed = true
	}
	if changed {
		stopLoomyOpencodeProcess(root)
	}
}

func syncLoomySidecar(root string, opencodeRaw []byte) bool {
	if len(opencodeRaw) == 0 {
		return false
	}
	sidecarPath := filepath.Join(root, loomySidecarFile)
	st, err := os.Lstat(sidecarPath)
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	sidecarBytes, err := steady.ReadThere(sidecarPath)
	if err != nil {
		return false
	}
	sidecarDoc, err := readJSON(sidecarBytes)
	if err != nil {
		return false
	}
	doc, err := readJSON(opencodeRaw)
	if err != nil {
		return false
	}
	model, _ := doc["model"].(string)
	var set []edit.KV
	var del []string
	if loomyRouterSelection(model) {
		if enabled, ok := sidecarDoc["customModelsEnabled"].(bool); !ok || !enabled {
			set = append(set, edit.KV{Path: "customModelsEnabled", Value: true})
		}
		if selected, ok := sidecarDoc["modelUserSelected"].(bool); !ok || !selected {
			set = append(set, edit.KV{Path: "modelUserSelected", Value: true})
		}
		if backup, _ := sidecarDoc["customModelBackup"].(string); backup != model {
			set = append(set, edit.KV{Path: "customModelBackup", Value: model})
		}
	} else if backup, ok := sidecarDoc["customModelBackup"].(string); ok && loomyRouterSelection(backup) {
		if strings.TrimSpace(model) != "" {
			set = append(set, edit.KV{Path: "customModelBackup", Value: model})
		} else {
			del = append(del, "customModelBackup")
		}
	}
	if len(set) == 0 && len(del) == 0 {
		return false
	}
	patched, err := edit.PatchJSON(sidecarBytes, set, del)
	if err != nil || equal(sidecarBytes, patched) {
		return false
	}
	return steady.WriteFile(sidecarPath, patched, st.Mode().Perm()) == nil
}

func stopLoomyOpencodeProcess(root string) {
	cleanRoot := filepath.Clean(root)
	parent := filepath.Dir(cleanRoot)
	if parent == cleanRoot || parent == "." || parent == "" {
		return
	}
	runtimeExe := filepath.Join(parent, "runtime", loomyOpencodeExecutableName())
	terminateExecutableProcesses(runtimeExe, isRealLoomyConfigRoot(cleanRoot))
}

func isRealLoomyConfigRoot(cleanRoot string) bool {
	slashed := "/" + strings.Trim(strings.ToLower(filepath.ToSlash(cleanRoot)), "/") + "/"
	nativeRoot := strings.HasSuffix(slashed, "/.config/loomy-opencode/")
	if !nativeRoot && !strings.Contains(slashed, "/loomy/") && !strings.Contains(slashed, "/.loomy/") {
		return false
	}
	for _, marker := range []string{"/tmp/", "/private/tmp/", "/var/folders/", "/private/var/folders/", "/appdata/local/temp/"} {
		if strings.Contains(slashed, marker) {
			return false
		}
	}
	tempDir := filepath.Clean(os.TempDir())
	if tempDir != "" && tempDir != "." {
		if isUnderDir(cleanRoot, tempDir) {
			return false
		}
		realTemp, err1 := filepath.EvalSymlinks(tempDir)
		realRoot, err2 := filepath.EvalSymlinks(cleanRoot)
		if err1 == nil && isUnderDir(cleanRoot, realTemp) {
			return false
		}
		if err1 == nil && err2 == nil && isUnderDir(realRoot, realTemp) {
			return false
		}
	}
	return true
}

func isUnderDir(child, parent string) bool {
	if runtime.GOOS == "windows" {
		child = strings.ToLower(filepath.Clean(child))
		parent = strings.ToLower(filepath.Clean(parent))
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
