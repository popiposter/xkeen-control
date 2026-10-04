package xkeen

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrCommand = errors.New("unsupported native command or parameter")

// CommandRequest is a typed operation, never a command line or executable path.
type CommandRequest struct {
	Action    string `json:"action"`
	Parameter string `json:"parameter,omitempty"`
}

type CommandSpec struct {
	Action      string `json:"action"`
	Label       string `json:"label"`
	Interactive bool   `json:"interactive"`
	Parameter   string `json:"parameter,omitempty"`
	flag        string
	limit       time.Duration
}

func CommandCatalog() []CommandSpec {
	return []CommandSpec{
		{Action: "start", Label: "Start", flag: "-start"},
		{Action: "stop", Label: "Stop", flag: "-stop"},
		{Action: "restart", Label: "Restart", flag: "-restart"},
		{Action: "status", Label: "Check status", flag: "-status"},
		{Action: "update-xkeen", Label: "Update XKeen", flag: "-uk", Interactive: true, limit: 15 * time.Minute},
		{Action: "update-xray", Label: "Update Xray", flag: "-ux", Interactive: true, Parameter: "version", limit: 15 * time.Minute},
		{Action: "update-geodata", Label: "Update geodata", flag: "-ug", Interactive: true, limit: 15 * time.Minute},
		{Action: "geodata-sources", Label: "Choose geodata sources", flag: "-g", Interactive: true, limit: 15 * time.Minute},
		{Action: "geodata-schedule", Label: "Set geodata schedule", flag: "-ugc", Interactive: true},
		{Action: "geodata-schedule-remove", Label: "Remove geodata schedule", flag: "-dgc", Interactive: true},
		{Action: "channel", Label: "Choose XKeen channel", flag: "-channel", Interactive: true},
		{Action: "autostart", Label: "Autostart", flag: "-auto", Parameter: "state"},
		{Action: "dns-interception", Label: "DNS interception", flag: "-dns", Parameter: "state"},
		{Action: "router-proxy", Label: "Proxy Entware traffic", flag: "-pr", Parameter: "state"},
		{Action: "pbr", Label: "Strict policy routing", flag: "-pbr", Parameter: "state"},
		{Action: "pbr-status", Label: "Policy routing status", flag: "-pbr"},
		{Action: "killswitch", Label: "Killswitch", flag: "-killswitch", Parameter: "state"},
		{Action: "killswitch-status", Label: "Killswitch status", flag: "-killswitch"},
		{Action: "speed-balancer", Label: "Speed balancer menu", flag: "-sb", Interactive: true},
		{Action: "speed-balancer-on", Label: "Enable speed balancer (includes measurement)", flag: "-sb", Interactive: true},
		{Action: "speed-balancer-off", Label: "Disable speed balancer", flag: "-sb"},
		{Action: "speed-balancer-status", Label: "Speed balancer status", flag: "-sb"},
		{Action: "backup-xkeen", Label: "Back up XKeen", flag: "-kb"},
		{Action: "backup-xray", Label: "Back up Xray configuration", flag: "-xb"},
		{Action: "restore-xray", Label: "Restore Xray configuration", flag: "-xbr", Interactive: true},
		{Action: "test-xray", Label: "Validate Xray configuration", flag: "-xtest"},
		{Action: "ports", Label: "Show proxy ports", flag: "-cp"},
		{Action: "excluded-ports", Label: "Show excluded ports", flag: "-cpe"},
		{Action: "ports-add", Label: "Add proxy ports", flag: "-ap", Parameter: "ports"},
		{Action: "ports-remove", Label: "Remove proxy ports", flag: "-dp", Parameter: "ports"},
		{Action: "excluded-ports-add", Label: "Add excluded ports", flag: "-ape", Parameter: "ports"},
		{Action: "excluded-ports-remove", Label: "Remove excluded ports", flag: "-dpe", Parameter: "ports"},
		{Action: "listen-ports", Label: "Show listening ports", flag: "-tp"},
		{Action: "help", Label: "Native XKeen help", flag: "-h"},
	}
}

var versionArgument = regexp.MustCompile(`^v?[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}$`)

func commandArguments(r CommandRequest) (CommandSpec, []string, error) {
	for _, s := range CommandCatalog() {
		if s.Action != r.Action {
			continue
		}
		args := []string{s.flag}
		switch s.Parameter {
		case "state":
			if r.Parameter != "on" && r.Parameter != "off" {
				return s, nil, ErrCommand
			}
			args = append(args, r.Parameter)
		case "version":
			if r.Parameter == "" || r.Parameter == "auto" {
				args = append(args, "auto")
			} else if versionArgument.MatchString(r.Parameter) {
				args = append(args, "v"+strings.TrimPrefix(r.Parameter, "v"))
			} else {
				return s, nil, ErrCommand
			}
		case "ports":
			ports := strings.Fields(r.Parameter)
			if len(ports) == 0 || len(ports) > 32 {
				return s, nil, ErrCommand
			}
			for _, p := range ports {
				for _, char := range p {
					if (char < '0' || char > '9') && char != ':' {
						return s, nil, ErrCommand
					}
				}
				bounds := strings.Split(p, ":")
				if len(bounds) > 2 {
					return s, nil, ErrCommand
				}
				previous := 0
				for _, b := range bounds {
					n, err := strconv.Atoi(b)
					if err != nil || n < 1 || n > 65535 || n < previous {
						return s, nil, ErrCommand
					}
					previous = n
				}
			}
			args = append(args, ports...)
		default:
			if r.Parameter != "" {
				return s, nil, ErrCommand
			}
		}
		switch s.Action {
		case "pbr-status", "killswitch-status", "speed-balancer-status":
			args = append(args, "status")
		case "speed-balancer-on":
			args = append(args, "on")
		case "speed-balancer-off":
			args = append(args, "off")
		}
		if s.limit == 0 {
			s.limit = 90 * time.Second
			if s.Interactive {
				s.limit = 15 * time.Minute
			}
			switch s.Action {
			case "status", "pbr-status", "killswitch-status", "speed-balancer-status", "ports", "excluded-ports", "listen-ports", "help":
				s.limit = 10 * time.Second
			}
		}
		return s, args, nil
	}
	return CommandSpec{}, nil, ErrCommand
}
