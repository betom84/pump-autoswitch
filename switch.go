package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Switch struct {
	m                sync.Mutex
	prefix           string
	client           mqtt.Client
	delayedOffCancel context.CancelFunc

	OnSwitchOffline func()

	lastOutput      bool
	OnSwitchUpdated func(bool)
}

func NewSwitch(c mqtt.Client, prefix string) (*Switch, error) {
	s := &Switch{}
	s.client = c
	s.prefix = prefix

	err := s.monitorOnline()
	if err != nil {
		return nil, err
	}

	err = s.monitorInput()
	if err != nil {
		return nil, err
	}

	err = s.monitorSwitch()
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Switch) monitorOnline() error {
	token := s.client.Subscribe(fmt.Sprintf("%s/online", s.prefix), byte(1), func(c mqtt.Client, m mqtt.Message) {
		slog.Warn("switch online state changed", slog.String("online", string(m.Payload())))
		m.Ack()

		if s.OnSwitchOffline != nil {
			go s.OnSwitchOffline()
		}
	})
	token.Wait()

	return token.Error()
}

func (s *Switch) monitorInput() error {
	var input struct {
		State bool `json:"state"`
	}

	token := s.client.Subscribe(fmt.Sprintf("%s/status/input:0", s.prefix), byte(1), func(c mqtt.Client, m mqtt.Message) {
		err := json.NewDecoder(bytes.NewReader(m.Payload())).Decode(&input)
		if err != nil {
			slog.Error("failed to parse message", slog.String("topic", m.Topic()), slog.Any("error", err))
			return
		}

		slog.Info("switch input state updated", slog.Bool("state", input.State))
		m.Ack()
	})
	token.Wait()

	return token.Error()
}

func (s *Switch) monitorSwitch() error {
	var sw struct {
		Output bool `json:"output"`
	}

	token := s.client.Subscribe(fmt.Sprintf("%s/status/switch:0", s.prefix), byte(1), func(c mqtt.Client, m mqtt.Message) {
		err := json.NewDecoder(bytes.NewReader(m.Payload())).Decode(&sw)
		if err != nil {
			slog.Error("failed to parse message", slog.String("topic", m.Topic()), slog.Any("error", err))
			return
		}

		m.Ack()

		s.m.Lock()
		defer s.m.Unlock()

		// shelly switches with power measurement update state once every minute,
		// so check for actual output change to reduce noise
		if s.lastOutput != sw.Output {
			slog.Info("switch output state updated", slog.Bool("output", sw.Output))
			s.lastOutput = sw.Output

			if s.OnSwitchUpdated != nil {
				go s.OnSwitchUpdated(sw.Output)
			}
		} else {
			slog.Debug("switch output state updated (ignored)", slog.Bool("output", sw.Output), slog.Bool("lastOutput", s.lastOutput))
		}
	})
	token.Wait()

	return token.Error()
}

func (s *Switch) On() error {
	s.m.Lock()
	defer s.m.Unlock()

	if s.delayedOffCancel != nil {
		s.delayedOffCancel()
	}

	return s.publish(true)
}

func (s *Switch) Off(ctx context.Context, delay time.Duration) {
	s.m.Lock()
	defer s.m.Unlock()

	if s.delayedOffCancel != nil {
		s.delayedOffCancel()
	}

	delayedCtx, delayedOffCancel := context.WithDeadline(ctx, time.Now().Add(delay))
	s.delayedOffCancel = func() {
		slog.Debug("delayed off-operation cancelled")
		delayedOffCancel()
	}

	go func() {
		<-delayedCtx.Done()

		s.m.Lock()
		defer s.m.Unlock()

		if delayedCtx.Err() == context.DeadlineExceeded {
			s.publish(false)
		}

		delayedOffCancel()
		s.delayedOffCancel = nil
	}()
}

func (s *Switch) publish(enabled bool) error {
	payload := "off"
	if enabled {
		payload = "on"
	}

	slog.Debug("publishing switch command", slog.String("payload", payload))

	token := s.client.Publish(fmt.Sprintf("%s/command/switch:0", s.prefix), byte(1), false, payload)
	if token.Wait() != true {
		slog.Error("failed to publish message", slog.Any("error", token.Error()))
	}

	return token.Error()
}
