package homeaudioclient

import (
	"context"
	"fmt"

	"github.com/function61/gokit/net/http/ezhttp"
	"github.com/joonas-fi/home-audio/pkg/homeaudiotypes"
)

const (
	Localhost = "http://localhost"
	HomeFn61  = "https://home.fn61.net"
)

type Client struct {
	baseURL string // base URL of the server
}

func New(baseURL string) *Client {
	return &Client{baseURL: baseURL}
}

func (c *Client) Speak(ctx context.Context, phrase string) error {
	if _, err := ezhttp.Post(ctx, c.baseURL+"/home-audio/api/speak", ezhttp.SendJSON(homeaudiotypes.SpeakInput{
		Phrase: phrase,
	})); err != nil {
		return fmt.Errorf("Speak: %w", err)
	}
	return nil
}
