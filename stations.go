package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Stations struct {
	state map[string]bool
	m     sync.Mutex

	OnStationUpdate       func(*Stations)
	OnStationsUnavailable func()
}

func NewStations(c mqtt.Client) (*Stations, error) {
	s := &Stations{}

	s.state = map[string]bool{
		"opensprinkler/station/0": false,
		"opensprinkler/station/1": false,
		"opensprinkler/station/2": false,
		"opensprinkler/station/3": false,
		"opensprinkler/station/4": false,
		"opensprinkler/station/5": false,
		"opensprinkler/station/6": false,
		"opensprinkler/station/7": false,
	}

	err := s.subscribe(c)
	if err != nil {
		return nil, err
	}

	err = s.monitorAvailability(c)

	return s, err
}
func (s *Stations) monitorAvailability(c mqtt.Client) error {
	token := c.Subscribe("opensprinkler/availability", byte(1), func(c mqtt.Client, m mqtt.Message) {
		availability := string(m.Payload())

		slog.Warn("opensprinkler availability state changed", slog.String("availaility", availability))
		m.Ack()

		if s.OnStationsUnavailable != nil && availability != "online" {
			go s.OnStationsUnavailable()
		}
	})
	token.Wait()

	return token.Error()
}

func (s *Stations) subscribe(c mqtt.Client) error {
	stations := make(map[string]byte)
	for t := range s.state {
		stations[t] = byte(1)
	}

	token := c.SubscribeMultiple(stations, func(c mqtt.Client, m mqtt.Message) { go s.HandleMessage(m) })
	token.Wait()

	return token.Error()
}

func (s *Stations) HandleMessage(msg mqtt.Message) bool {
	if _, ok := s.state[msg.Topic()]; !ok {
		return false
	}

	var payload struct {
		State int `json:"state"`
	}

	err := json.NewDecoder(bytes.NewReader(msg.Payload())).Decode(&payload)
	if err != nil {
		slog.Error("failed to parse message", slog.Any("error", err))
		return false
	}

	s.m.Lock()
	s.state[msg.Topic()] = payload.State == 1
	s.m.Unlock()

	if s.OnStationUpdate != nil {
		s.OnStationUpdate(s)
	}

	return true
}

func (s *Stations) IsStationEnabled() bool {
	s.m.Lock()
	defer s.m.Unlock()

	for _, e := range s.state {
		if e {
			return true
		}
	}

	return false
}
