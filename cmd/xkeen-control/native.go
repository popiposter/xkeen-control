package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func runNativeCommand(args []string, output io.Writer, discovery xkeen.Discovery) error {
	if len(args) != 2 || args[0] != "inspect" || args[1] != "--json" {
		return errors.New("usage: xkeen-control native inspect --json")
	}
	return json.NewEncoder(output).Encode(discovery.Inspect(context.Background()))
}
