package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

var (
	logLevel      = flag.String("logLevel", "INFO", "DEBUG, INFO, WARN, ERROR")
	broker        = flag.String("broker", "tcp://sarah.fritz.box:1883", "MQTT broker URL")
	pushoverUser  = flag.String("pushoverUser", "", "User for Pushover notifications")
	pushoverToken = flag.String("pushoverToken", "", "Token for Pushover notifications")
)

type StationState struct {
	station string
	state   bool
}

func main() {
	flag.Parse()
	lvl := &slog.LevelVar{}
	lvl.UnmarshalText([]byte(*logLevel))
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
	})))

	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	messages := make(chan MQTT.Message)

	opts := MQTT.NewClientOptions()
	opts.AddBroker(*broker)
	opts.SetClientID("pump-autoswitch")
	opts.SetDefaultPublishHandler(func(client MQTT.Client, msg MQTT.Message) { messages <- msg })
	opts.OnConnect = func(c MQTT.Client) { slog.Info("mqtt client connected") }
	opts.OnConnectionLost = func(c MQTT.Client, err error) { slog.Error("mqtt connection lost", slog.Any("error", err)) }
	opts.OnReconnecting = func(c MQTT.Client, co *MQTT.ClientOptions) { slog.Info("mqtt client reconnecting") }

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	switcher := newPumpSwitcher(ctx, client)
	stations := map[string]byte{
		"opensprinkler/station/0": byte(1),
		"opensprinkler/station/1": byte(1),
		"opensprinkler/station/2": byte(1),
		"opensprinkler/station/3": byte(1),
		"opensprinkler/station/4": byte(1),
		"opensprinkler/station/5": byte(1),
		"opensprinkler/station/6": byte(1),
		"opensprinkler/station/7": byte(1),
		"shellies/pump/relay/0":   byte(1), // for debugging purposes
	}

	if token := client.SubscribeMultiple(stations, nil); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

loop:
	for {
		select {
		case msg := <-messages:
			slog.Debug("mqtt message incomming", slog.String("topic", msg.Topic()), slog.String("payload", string(msg.Payload())))

			if strings.HasPrefix(msg.Topic(), "opensprinkler/station") {
				var payload struct {
					State int `json:"state"`
				}

				err := json.NewDecoder(bytes.NewReader(msg.Payload())).Decode(&payload)
				if err != nil {
					slog.Error("failed to parse message", slog.Any("error", err))
					continue
				}

				switcher <- StationState{msg.Topic(), payload.State == 1}
			}

			msg.Ack()

		case <-ctx.Done():
			break loop
		}
	}
}

func newPumpSwitcher(ctx context.Context, client MQTT.Client) chan<- StationState {
	stationStates := make(map[string]bool)
	states := make(chan StationState, 10)

	duration := 5 * time.Second
	ticker := time.NewTicker(duration)

	go func() {
		isPumpActive := false
		for {
			select {
			case state := <-states:
				slog.Debug("station state updated", slog.String("station", state.station), slog.Bool("state", state.state))

				stationStates[state.station] = state.state
				ticker.Reset(duration)

				if !state.state {
					continue
				}

			case <-ticker.C:
				break

			case <-ctx.Done():
				switchPump(client, false)
				close(states)
				return
			}

			p := false
			for _, s := range stationStates {
				p = p || s
			}

			if p == isPumpActive {
				continue
			}

			err := switchPump(client, p)
			if err != nil {
				notify("Failed to switch pump!")
				continue
			}

			message := "Pump turned off"
			if p {
				message = "Pump turned on"
			}

			notify(message)
			isPumpActive = p
		}
	}()

	return states
}

func switchPump(client MQTT.Client, active bool) error {
	payload := "off"
	if active {
		payload = "on"
	}

	slog.Debug("switching pump", slog.String("payload", payload))

	token := client.Publish("shellies/pump/relay/0/command", byte(1), false, payload)
	if token.Wait() != true {
		slog.Error("failed to publish message", slog.Any("error", token.Error()))
	}

	return token.Error()
}

func notify(message string) error {
	var payload = struct {
		Token   string `json:"token"`
		User    string `json:"user"`
		Message string `json:"message"`
	}{
		Token:   *pushoverToken,
		User:    *pushoverUser,
		Message: message,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal pushover message", slog.Any("message", payload.Message), slog.Any("error", err))
		return err
	}

	resp, err := http.Post("https://api.pushover.net/1/messages.json", "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Error("failed to post pushover message", slog.Any("message", payload.Message), slog.Any("error", err))
		return err
	}

	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		slog.Error("failed to post pushover message",
			slog.Any("message", payload.Message),
			slog.Int("code", resp.StatusCode),
			slog.String("response", string(respBody)),
			slog.Any("error", err),
		)

		return fmt.Errorf(resp.Status)
	}

	slog.Info("pushover notification successful", slog.String("message", payload.Message))
	return nil
}
