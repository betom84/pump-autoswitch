package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

func Info(message string) {
	Notify(message, 0)
}

func Critical(message string) {
	Notify(message, 2)
}

func Notify(message string, priority int) error {
	var payload = struct {
		Token    string `json:"token"`
		User     string `json:"user"`
		Message  string `json:"message"`
		TTL      int    `json:"ttl"`
		Expire   int    `json:"expire"`
		Retry    int    `json:"retry"`
		Priority int    `json:"priority"`
	}{
		Token:    *pushoverToken,
		User:     *pushoverUser,
		Message:  message,
		TTL:      int((24 * time.Hour).Seconds()),
		Expire:   int((1 * time.Hour).Seconds()),
		Retry:    int((15 * time.Minute).Seconds()),
		Priority: priority,
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

		return errors.New(resp.Status)
	}

	slog.Info("pushover notification successful", slog.String("message", payload.Message))
	return nil
}
