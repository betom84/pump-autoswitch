package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var (
	logLevel      = flag.String("logLevel", "INFO", "DEBUG, INFO, WARN, ERROR")
	broker        = flag.String("broker", "tcp://sarah.fritz.box:1883", "MQTT broker URL")
	clientID      = flag.String("clientID", "pump-autoswitch", "MQTT client ID")
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

	client, err := connectMQTT()
	if err != nil {
		panic(err)
	}
	defer client.Disconnect(1000)

	shelly, err := NewSwitch(client, "shelly-pump")
	if err != nil {
		panic(fmt.Errorf("failed to init switch; %v", err))
	}

	shelly.OnSwitchUpdated = func(enabled bool) {
		if enabled {
			Info("Pumpe eingeschalten")
		} else {
			Info("Pumpe ausgeschalten")
		}
	}

	shelly.OnSwitchOffline = func() {
		Critical("Schalter für Pumpe nicht erreichbar!")
	}

	stations, err := NewStations(client)
	if err != nil {
		panic(fmt.Errorf("failed to init stations; %v", err))
	}

	stations.OnStationUpdate = func(s *Stations) {
		if s.IsStationEnabled() {
			shelly.On()
		} else {
			shelly.Off(ctx, 10*time.Second)
		}
	}

	stations.OnStationsUnavailable = func() {
		Critical("OpenSprinkler Steuerung nicht erreichbar!")
	}

	<-ctx.Done()
}

func connectMQTT() (mqtt.Client, error) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(*broker)
	opts.SetClientID(*clientID)
	opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		slog.Debug("mqtt message incomming", slog.String("topic", msg.Topic()), slog.String("payload", string(msg.Payload())))
	})

	opts.OnConnect = func(c mqtt.Client) { slog.Info("mqtt client connected") }
	opts.OnConnectionLost = func(c mqtt.Client, err error) { slog.Error("mqtt connection lost", slog.Any("error", err)) }
	opts.OnReconnecting = func(c mqtt.Client, co *mqtt.ClientOptions) { slog.Info("mqtt client reconnecting") }

	client := mqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()

	return client, token.Error()
}
