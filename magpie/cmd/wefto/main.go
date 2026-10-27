// Wefto starts independently of Magpie's migrations and background jobs.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerservice"
)

var version = "dev"

func main() {
	ctx := context.Background()
	if len(os.Args) >= 2 && (os.Args[1] == "diagnose-local" || os.Args[1] == "serve") {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
	}
	if err := runContext(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "wefto:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	return runContext(context.Background(), args, out)
}

func runContext(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "help" || args[0] == "--help")) {
		_, err := fmt.Fprintln(out, "Usage: wefto <install|uninstall|service|serve [--experimental-redaction]|healthcheck|version|dirs|config init|config check|host|diagnose-local>\nUse wefto install --help for fixed Windows package installation and safe uninstall.\nUse wefto service --help for hidden start, stop, restart, status and explicit login autostart.\nserve starts the foreground local service and embedded management page. Stop with Ctrl+C or SIGTERM.\n--experimental-redaction permits an explicitly labeled X2.6 Q4_K_M/Q8_0 redaction trial with a 30-second total budget; recognition quality is not formally accepted. Invalid results still block cloud requests.\nUse wefto host --help for host configuration preview, apply and offline restore.\nUse wefto diagnose-local --help for the fixed local diagnostic suite.")
		return err
	}
	if len(args) == 1 && args[0] == "version" {
		_, err := fmt.Fprintln(out, version)
		return err
	}
	if args[0] == "host" {
		return runHost(ctx, args[1:], out)
	}
	if args[0] == "service" {
		return runService(ctx, args[1:], out)
	}
	if args[0] == "install" || args[0] == "uninstall" {
		return runDelivery(ctx, args[0], args[1:], out)
	}
	if args[0] == "diagnose-local" {
		return runDiagnoseLocal(ctx, args[1:], out)
	}
	command := ""
	experimentalRedaction := len(args) == 2 && args[0] == "serve" && args[1] == "--experimental-redaction"
	if len(args) == 1 && (args[0] == "dirs" || args[0] == "serve" || args[0] == "healthcheck") {
		command = args[0]
	} else if experimentalRedaction {
		command = "serve"
	} else if len(args) == 2 && args[0] == "config" && (args[1] == "init" || args[1] == "check") {
		command = args[1]
	} else {
		return fmt.Errorf("command disabled in wefto; use wefto --help")
	}
	layout, err := dirs.Resolve()
	if err != nil {
		return err
	}
	if command == "dirs" {
		e := json.NewEncoder(out)
		e.SetIndent("", "  ")
		return e.Encode(layout)
	}
	if command == "serve" {
		return routerservice.RunWithModelOptions(ctx, layout, version, func(info routerservice.Info) error {
			_, err := fmt.Fprintf(out, "Wefto %s\nManagement: http://%s/\nManagement token file: %s\n", version, info.Address, routerservice.TokenFile(layout))
			if err == nil && experimentalRedaction {
				_, err = fmt.Fprintln(out, "Experimental redaction trial: enabled; 30-second total budget; recognition quality not formally accepted. Invalid results block cloud requests.")
			}
			return err
		}, routerservice.ModelOptions{ExperimentalRedaction: experimentalRedaction})
	}
	if command == "healthcheck" {
		if err := routerservice.Healthcheck(ctx, layout); err != nil {
			return err
		}
		_, err := fmt.Fprintln(out, "wefto: ok")
		return err
	}
	store := routerconfig.New(layout)
	if command == "init" {
		_, err = store.InitializeContext(ctx)
	} else {
		_, err = store.LoadContext(ctx)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, layout.ConfigFile())
	return err
}
