package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Command is a closed bot vocabulary, never a command line or prompt answer.
type Command string

const (
	Help                 Command = "/help"
	StatusCommand        Command = "/status"
	StartCommand         Command = "/start"
	StopCommand          Command = "/stop"
	RestartCommand       Command = "/restart"
	UpdateXkeen          Command = "/update_xkeen"
	UpdateXray           Command = "/update_xray"
	UpdateGeodata        Command = "/update_geodata"
	RefreshSubscriptions Command = "/refresh"
)

type ControlResult string

const (
	Running  ControlResult = "running"
	Stopped  ControlResult = "stopped"
	Unknown  ControlResult = "unknown"
	Accepted ControlResult = "accepted"
	Refused  ControlResult = "refused"
)

type ControlHandler func(context.Context, Command) ControlResult

func validUserID(value string) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == value
}

func (s *Service) SetControl(enabled bool, user string) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, state := readAuthority(s.path)
	if state != "configured" {
		return s.statusLocked(), Error(state)
	}
	if enabled && !validUserID(user) || user != "" && !validUserID(user) {
		return s.statusLocked(), Error("invalid-request")
	}
	value.ControlEnabled, value.AllowedUserID = enabled, user
	s.controlEpoch++
	err := writeAuthority(s.path, value)
	return s.statusLocked(), err
}

type botUpdate struct {
	ID      int64 `json:"update_id"`
	Message *struct {
		Date int64  `json:"date"`
		Text string `json:"text"`
		From struct {
			ID  int64 `json:"id"`
			Bot bool  `json:"is_bot"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Forward    json.RawMessage `json:"forward_origin"`
		SenderChat json.RawMessage `json:"sender_chat"`
	} `json:"message"`
}

func botCommand(update botUpdate, value authority, now time.Time) Command {
	m := update.Message
	if update.ID <= 0 || update.ID >= 1<<63-1 || m == nil || m.From.Bot || len(m.Forward) != 0 || len(m.SenderChat) != 0 || strconv.FormatInt(m.From.ID, 10) != value.AllowedUserID || strconv.FormatInt(m.Chat.ID, 10) != value.ChatID || m.Date > now.Unix() || now.Unix()-m.Date > 120 {
		return ""
	}
	switch Command(m.Text) {
	case Help, StatusCommand, StartCommand, StopCommand, RestartCommand, UpdateXkeen, UpdateXray, UpdateGeodata, RefreshSubscriptions:
		return Command(m.Text)
	}
	return ""
}

func (t *telegram) updates(ctx context.Context, value authority, offset int64, bootstrap bool) ([]botUpdate, error) {
	wait := 20
	if bootstrap {
		wait = 0
	}
	data, _ := json.Marshal(struct {
		Offset  int64    `json:"offset"`
		Limit   int      `json:"limit"`
		Timeout int      `json:"timeout"`
		Allowed []string `json:"allowed_updates"`
	}{offset, 16, wait, []string{"message"}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+telegramAddress+"/bot"+value.BotToken+"/getUpdates", bytes.NewReader(data))
	if err != nil {
		return nil, Error("transport-failed")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := t.client.Do(request)
	if err != nil {
		return nil, Error("transport-failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if err != nil || len(body) > 256<<10 || response.StatusCode != http.StatusOK {
		return nil, Error("provider-rejected")
	}
	var result struct {
		OK     bool        `json:"ok"`
		Result []botUpdate `json:"result"`
	}
	if json.Unmarshal(body, &result) != nil || !result.OK || len(result.Result) > 16 {
		return nil, Error("provider-rejected")
	}
	sort.Slice(result.Result, func(i, j int) bool { return result.Result[i].ID < result.Result[j].ID })
	return result.Result, nil
}

func controlText(command Command, result ControlResult) string {
	if command == Help {
		return "XKeen commands: /status /start /stop /restart /update_xkeen /update_xray /update_geodata /refresh. Native console and any prompts are available only in the panel. Pending configuration changes must be applied there."
	}
	switch result {
	case Running:
		return "XKeen engine: running. This does not verify client Internet or VPN reachability."
	case Stopped:
		return "XKeen engine: stopped."
	case Accepted:
		return "Native command accepted. Inspect final state and console in the panel; acceptance is not success."
	case Refused:
		return "Command refused: operation busy, pending configuration or unavailable command. Inspect the panel."
	default:
		return "State unknown. Inspect the panel; no command will be replayed."
	}
}

// consume persists the one update watermark BEFORE dispatch. A crash may lose a
// command, but never replays its mutation. Returned disable/clear cannot race a
// new dispatch from a stale poll. No console, provider error or config is sent.
func (s *Service) consume(ctx context.Context, snapshot authority, epoch uint64, update botUpdate, handler ControlHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, state := readAuthority(s.path)
	if state != "configured" || !value.ControlEnabled || s.controlEpoch != epoch || value.BotToken != snapshot.BotToken || value.ChatID != snapshot.ChatID || value.AllowedUserID != snapshot.AllowedUserID {
		return
	}
	command := botCommand(update, value, time.Now())
	if command == "" {
		return
	}
	// Telegram can choose a lower random ID after a week without updates.
	// Rebase only when every command in the old watermark's freshness window
	// has expired; their immutable message dates cannot pass this new window.
	if update.ID <= value.LastUpdateID && update.Message.Date <= value.LastMessageDate+120 {
		return
	}
	value.LastUpdateID = update.ID
	value.LastMessageDate = update.Message.Date
	if writeAuthority(s.path, value) != nil {
		s.status.ControlState = "failed"
		return
	}
	result := Unknown
	if command == Help {
		result = Accepted
	} else if handler != nil {
		result = handler(ctx, command)
	}
	if s.transport.send(ctx, value, controlText(command, result)) != nil {
		s.status.ControlState = "failed"
	} else {
		s.status.ControlState = "listening"
	}
}

// RunControl has one caller in main and is off until explicitly configured.
// A fresh receiver drops queued commands before listening. Poll offsets live in
// RAM; only accepted operator commands cause a bounded persistent watermark write.
func (s *Service) RunControl(ctx context.Context, handler ControlHandler) {
	poller := newTelegramWithTimeout(25 * time.Second)
	var token, chat, user string
	var generation uint64
	offset := int64(-1)
	lastBatch := time.Now()
	for ctx.Err() == nil {
		s.mu.Lock()
		value, state := readAuthority(s.path)
		epoch := s.controlEpoch
		s.mu.Unlock()
		active := state == "configured" && value.ControlEnabled && validUserID(value.AllowedUserID)
		if !active {
			token, chat, user = "", "", ""
			offset = -1
			if !controlPause(ctx, 5*time.Second) {
				return
			}
			continue
		}
		fresh := epoch != generation || value.BotToken != token || value.ChatID != chat || value.AllowedUserID != user
		if fresh {
			offset = -1
		} else if time.Since(lastBatch) > 120*time.Second {
			// A higher offset can otherwise hide a new random ID indefinitely.
			// The previous batch is already confirmed and outside command TTL.
			offset = 0
		}
		updates, err := poller.updates(ctx, value, offset, fresh)
		if err != nil {
			s.mu.Lock()
			if s.controlEpoch == epoch {
				s.status.ControlState = "failed"
			}
			s.mu.Unlock()
			if !controlPause(ctx, 10*time.Second) {
				return
			}
			continue
		}
		token, chat, user = value.BotToken, value.ChatID, value.AllowedUserID
		generation = epoch
		if fresh {
			offset = 0
		}
		if len(updates) != 0 {
			lastBatch = time.Now()
		}
		s.mu.Lock()
		if s.controlEpoch == epoch {
			s.status.ControlState = "listening"
		}
		s.mu.Unlock()
		for _, update := range updates {
			if update.ID <= 0 || update.ID >= 1<<63-1 {
				continue
			}
			if !fresh {
				s.consume(ctx, value, epoch, update, handler)
			}
			if update.ID >= offset {
				offset = update.ID + 1
			}
		}
	}
}
func controlPause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
