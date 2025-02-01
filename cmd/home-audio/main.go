package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/function61/gokit/app/cli"
	. "github.com/function61/gokit/builtin"
	"github.com/function61/gokit/net/http/ezhttp"
	"github.com/function61/gokit/os/osutil"
	"github.com/joonas-fi/home-audio/pkg/homeaudioclient"
	"github.com/spf13/cobra"
)

func main() {
	app := &cobra.Command{
		Short: "Home audio control",
	}

	cmd := &cobra.Command{
		Use:   "speak [phrase]",
		Short: "Speak a phrase on a device",
		Args:  cobra.ExactArgs(1),
		Run: cli.WrapRun(func(ctx context.Context, args []string) error {
			return speakCLI(ctx, args[0])
		}),
	}
	cli.AddLogLevelControls(cmd.Flags())
	app.AddCommand(cmd)

	app.AddCommand(&cobra.Command{
		Use:   "server",
		Short: "Start HTTP server",
		Args:  cobra.NoArgs,
		Run: cli.WrapRun(func(ctx context.Context, args []string) error {
			return server(ctx)
		}),
	})

	app.AddCommand(&cobra.Command{
		Use:   "client-speak",
		Short: "Use the API client to speak to the server",
		Args:  cobra.NoArgs,
		Run: cli.WrapRun(func(ctx context.Context, args []string) error {
			client := homeaudioclient.New(homeaudioclient.Localhost)
			return client.Speak(ctx, "testing my good mate")
		}),
	})

	cli.Execute(app)
}

func speakCLI(ctx context.Context, phrase string) error {
	if err := ErrorIfUnset(phrase == "", "phrase"); err != nil {
		return err
	}

	effects := defaultEffects()

	speechDownloadURL, err := effects.TextToSpeech(ctx, phrase)
	if err != nil {
		return err
	}

	slog.Debug("got URL", "url", speechDownloadURL)

	if err := effects.PlayAudio(ctx, speechDownloadURL); err != nil {
		return err
	}
	return nil
}

// func playSpeechURLLocally(ctx context.Context, speechDownloadURL string) error {
// 	const filenameTemp = "tmp.mp3"
// 	if err := downloadFileLocally(ctx, speechDownloadURL, filenameTemp); err != nil {
// 		return err
// 	}
// 	if output, err := exec.CommandContext(ctx, "paplay", filenameTemp).CombinedOutput(); err != nil {
// 		return fmt.Errorf("paplay: %w: %s", err, string(output))
// 	}
// 	return nil
// }

// func downloadFileLocally(ctx context.Context, speechDownloadURL string, filenameTemp string) error {
// 	resp, err := ezhttp.Get(ctx, speechDownloadURL)
// 	if err != nil {
// 		return err
// 	}
// 	defer resp.Body.Close()

// 	return osutil.WriteFileAtomicFromReader(filenameTemp, resp.Body)
// }

func playUsingScreenServerClientScreenWall(ctx context.Context, speechDownloadURL string) error {
	const screenWallIP = "192.168.2.6"
	return playUsingScreenServerClient(ctx, screenWallIP, speechDownloadURL)
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
	if _, err := server.Write([]byte(fmt.Sprintf("%s\n", strings.Join(cmd, ",")))); err != nil {
		return err
	}

	return nil
}

func makeSpeech(ctx context.Context, phrase string) (string, error) {
	withErr := func(err error) (string, error) { return "", fmt.Errorf("makeSpeech: %w", err) }

	if err := ErrorIfUnset(phrase == "", "phrase"); err != nil {
		return withErr(err)
	}

	baseURL := "https://home-assistant.home.fn61.net"
	baseURL2 := "http://home.fn61.net:5901"
	authToken, err := osutil.GetenvRequired("HOME_ASSISTANT_TOKEN")
	if err != nil {
		return withErr(err)
	}

	// depends on Home Assistant config
	const engineID = "tts.google_en_com"

	req := struct {
		Message  string `json:"message"`
		EngineID string `json:"engine_id"`
	}{
		Message:  phrase,
		EngineID: engineID,
	}
	res := struct {
		// the baseURL of this depends on autodecetion logic (probably wrong in container) or an "external URL" possibly can be set but it needs to be fixed for one perspective
		// (which might not be enough for us)
		URL  string `json:"url"`
		Path string `json:"path"`
	}{}

	if _, err := ezhttp.Post(ctx, baseURL+"/api/tts_get_url", ezhttp.AuthBearer(authToken), ezhttp.SendJSON(req), ezhttp.RespondsJSONAllowUnknownFields(&res)); err != nil {
		return withErr(err)
	}

	speechDownloadURL := baseURL2 + res.Path

	return speechDownloadURL, nil
}
