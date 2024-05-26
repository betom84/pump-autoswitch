package main

import "testing"

func TestPushoverNotification(t *testing.T) {
	err := notify("Test")
	if err != nil {
		t.Fatalf(err.Error())
	}
}
