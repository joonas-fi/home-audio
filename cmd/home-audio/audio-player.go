package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	. "github.com/function61/gokit/builtin"
	"github.com/function61/gokit/net/http/ezhttp"
)

func playUsingScreenServerClientScreenWall(ctx context.Context, speechDownloadURL string) error {
	const screenWallIP = "192.168.2.6"
	return playUsingScreenServerClient(ctx, screenWallIP, speechDownloadURL)
}

func playUsingLocalhostHautomoClient(ctx context.Context, speechDownloadURL string) error {
	return playUsingHautomoClient(ctx, speechDownloadURL, "http://localhost:8084")
}

func playUsingWorkHautomoClient(ctx context.Context, speechDownloadURL string) error {
	return playUsingHautomoClient(ctx, speechDownloadURL, "http://192.168.1.104:8004")
}

func playUsingHautomoClient(ctx context.Context, speechDownloadURL string, addr string) error {
	withErr := FuncWrapErr

	formData := strings.NewReader(url.Values{
		"url": {speechDownloadURL},
	}.Encode())
	if _, err := ezhttp.Post(ctx, addr+"/api/audioplayback/play", ezhttp.SendBody(formData, "application/x-www-form-urlencoded")); err != nil {
		return withErr(err)
	}
	return nil
}

func playUsingScreenServerClient(ctx context.Context, ip string, speechDownloadURL string) error {
	server, err := (&net.Dialer{}).DialContext(ctx, "tcp", ip+":53000")
	if err != nil {
		return err
	}
	defer server.Close()

	serverReader := bufio.NewReader(server)
	greeting, isPrefix, err := serverReader.ReadLine()
	if err != nil {
		return err
	}
	if isPrefix {
		return errors.New("greeting read was prefix")
	}

	if string(greeting) != "sscs" {
		return fmt.Errorf("unexpected greeting: '%s'", string(greeting))
	}

	cmd := []string{"audio.play", speechDownloadURL}
	if _, err := fmt.Fprintf(server, "%s\n", strings.Join(cmd, ",")); err != nil {
		return err
	}

	return nil
}
