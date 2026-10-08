package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/popiposter/xkeen-control/internal/setup"
)

func runSetupCommand(args []string) error {
	if len(args) != 1 || os.Geteuid() != 0 {
		return errors.New("usage: root-only xkeen-control setup {run|inspect|guard|panel-guard|recover|abort}")
	}
	switch args[0] {
	case "guard":
		return setup.Guard()
	case "panel-guard":
		return setup.PanelInstallGuard()
	case "inspect":
		r, e := setup.Inspect()
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	case "run":
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return setup.Run(ctx, os.Stdin, os.Stdout)
	case "recover":
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return setup.Recover(ctx, os.Stdout)
	case "abort":
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return setup.Abort(ctx, os.Stdout)
	default:
		return errors.New("unsupported initial setup action")
	}
}
