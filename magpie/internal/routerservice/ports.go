package routerservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerhost"
)

// Bind first and retain the listener. A probe followed by closing/rebinding
// would race another process and could save an address we never actually own.
func listenAvailable(address string) (net.Listener, error) {
	listener, err := net.Listen("tcp", address)
	if err == nil {
		return listener, nil
	}
	if !listenUnavailable(err) {
		return nil, err
	}
	host, port, _ := net.SplitHostPort(address)
	n, _ := strconv.Atoi(port)
	for attempt := 0; attempt < 32; attempt++ {
		n++
		if n > 65535 {
			n = 49152
		}
		listener, err = net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(n)))
		if err == nil {
			return listener, nil
		}
		if !listenUnavailable(err) {
			return nil, err
		}
	}
	return net.Listen("tcp", net.JoinHostPort(host, "0"))
}

func startupPortTargets(ctx context.Context, store *routerconfig.Store, stateDir string) ([]routerhost.PortTarget, error) {
	apps, err := store.AppsContext(ctx)
	if err != nil {
		return nil, err
	}
	var targets []routerhost.PortTarget
	for _, app := range apps {
		if (app.ID != "workbuddy" && app.ID != "loomy") || app.CredentialMode != routerconfig.StoredCredential {
			continue
		}
		roots, err := routerhost.ConnectedRoots(stateDir, app.ID)
		if err != nil {
			return nil, err
		}
		if !app.Configured {
			for _, root := range roots {
				if connected, live := inspectHostLiveFiles(app.ID, root); connected && len(live) > 0 {
					targets = append(targets, routerhost.PortTarget{Host: app.ID, ConfigRoot: filepath.Clean(root), Model: live[0]})
				} else if app.ID == "loomy" {
					if m := loomyPortSyncModel(root); m != "" {
						targets = append(targets, routerhost.PortTarget{Host: app.ID, ConfigRoot: filepath.Clean(root), Model: m})
					}
				}
			}
			continue
		}
		root, discoverErr := autoHostConfigRoot(app.ID)
		if discoverErr == nil {
			roots = append(roots, root)
		} else if strings.Contains(discoverErr.Error(), "multiple") ||
			(app.ID == "loomy" && (os.Getenv("LOOMY_CONFIG_DIR") != "" || os.Getenv("LOOMY_OPENCODE_DIR") != "")) {
			if len(roots) == 0 {
				return nil, discoverErr
			}
		}
		model := app.Cloud.Model
		if model == "" && len(app.Models) > 0 {
			model = app.Models[0]
		}
		for _, root := range roots {
			targetModel := model
			if connected, live := inspectHostLiveFiles(app.ID, root); connected && len(live) > 0 && (targetModel == "" || app.ID == "loomy" || live[0] == app.Cloud.Model || (len(app.Models) > 0 && live[0] == app.Models[0])) {
				targetModel = live[0]
			}
			targets = append(targets, routerhost.PortTarget{Host: app.ID, ConfigRoot: filepath.Clean(root), Model: targetModel})
		}
	}
	return targets, nil
}

func loomyPortSyncModel(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "opencode.json"))
	var doc map[string]any
	if err != nil || json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	prov, _ := doc["provider"].(map[string]any)
	ar, _ := prov["api_router"].(map[string]any)
	opts, _ := ar["options"].(map[string]any)
	if opts["apiKey"] != routerhost.PlaceholderAPIKey {
		return ""
	}
	if mMap, ok := ar["models"].(map[string]any); ok {
		for k := range mMap {
			if strings.TrimSpace(k) != "" {
				return k
			}
		}
	}
	return "spark-x2.5"
}

func synchronizeStartupPort(ctx context.Context, store *routerconfig.Store, stateDir string, original routerconfig.Document, oldAddress, address string) error {
	targets, err := startupPortTargets(ctx, store, stateDir)
	if err != nil {
		return err
	}
	persist := func() error {
		check := func(current routerconfig.Document) error {
			actual, err := Address(current)
			if err != nil || actual != oldAddress || !bytes.Equal(current["apps"], original["apps"]) {
				return errors.New("Router configuration changed during startup; retry with the latest settings")
			}
			return nil
		}
		if oldAddress == address {
			current, err := store.LoadContext(ctx)
			if err != nil {
				return err
			}
			return check(current)
		}
		return store.UpdateContext(ctx, func(current routerconfig.Document) error {
			if err := check(current); err != nil {
				return err
			}
			var listen map[string]json.RawMessage
			if err := json.Unmarshal(current["listen"], &listen); err != nil {
				return err
			}
			listen["address"], _ = json.Marshal(address)
			current["listen"], _ = json.Marshal(listen)
			return nil
		})
	}
	if err := routerhost.SynchronizePorts(ctx, stateDir, "http://"+oldAddress, "http://"+address, targets, persist); err != nil {
		return fmt.Errorf("cannot synchronize the bound port with connected applications: %w", err)
	}
	return nil
}
