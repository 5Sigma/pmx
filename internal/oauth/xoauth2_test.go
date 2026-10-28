package oauth

import (
	"bytes"
	"testing"
)

func TestXOAuth2Client_Start(t *testing.T) {
	c := &XOAuth2Client{User: "test@example.com", Token: "mytoken"}

	mech, ir, err := c.Start()
	if err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}
	if mech != "XOAUTH2" {
		t.Errorf("mech: want %q, got %q", "XOAUTH2", mech)
	}
	want := []byte("user=test@example.com\x01auth=Bearer mytoken\x01\x01")
	if !bytes.Equal(ir, want) {
		t.Errorf("ir: want %q, got %q", want, ir)
	}
}

func TestXOAuth2Client_Next(t *testing.T) {
	c := &XOAuth2Client{User: "test@example.com", Token: "mytoken"}

	resp, err := c.Next(nil)
	if err != nil {
		t.Errorf("Next(nil) unexpected error: %v", err)
	}
	if resp != nil {
		t.Errorf("Next(nil) response: want nil, got %q", resp)
	}
}
