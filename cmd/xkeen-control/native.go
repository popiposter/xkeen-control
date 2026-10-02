package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func runNativeCommand(args []string, output io.Writer, discovery xkeen.Discovery) error {
	if len(args) != 2 || (args[0] != "inspect" && args[0] != "attachment-check") || args[1] != "--json" {
		return errors.New("usage: xkeen-control native {inspect|attachment-check} --json")
	}
	if args[0] == "attachment-check" {
		result, err := discovery.CheckAttachment(context.Background())
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(result)
	}
	return json.NewEncoder(output).Encode(discovery.Inspect(context.Background()))
}
