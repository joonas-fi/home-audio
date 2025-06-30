package main

import (
	"context"
	"log/slog"

	"github.com/function61/gokit/app/cli"
	. "github.com/function61/gokit/builtin"
	"github.com/joonas-fi/home-audio/pkg/homeaudioclient"
	"github.com/spf13/cobra"
)

func main() {
	app := &cobra.Command{
		Short: "Home audio control",
	}

	cmd := &cobra.Command{
		Use:   "speak [phrase]",
		Short: "TTS a phrase to get audio and then play that on a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return speakCLI(cmd.Context(), args[0])
		},
	}
	cli.AddLogLevelControls(cmd.Flags())
	app.AddCommand(cmd)

	app.AddCommand(&cobra.Command{
		Use:   "play-audio [url]",
		Short: "Play audio on a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return playCLI(cmd.Context(), args[0], defaultEffects())
		},
	})

	app.AddCommand(debugEntrypoint())

	app.AddCommand(&cobra.Command{
		Use:   "server",
		Short: "Start HTTP server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return server(cmd.Context())
		},
	})

	app.AddCommand(&cobra.Command{
		Use:   "client-speak",
		Short: "Use the API client to speak to the server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := homeaudioclient.New(homeaudioclient.Localhost)
			return client.Speak(cmd.Context(), "testing my good mate")
		},
	})

	app.AddCommand(&cobra.Command{
		Use:   "hautomo-local-speak [phrase]",
		Short: "Speak using hautomo-client to localhost",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			phrase := args[0]
			if err := ErrorIfUnset(phrase == "", "phrase"); err != nil {
				return err
			}

			audioURL, err := defaultEffects().TextToSpeechURL(cmd.Context(), phrase)
			if err != nil {
				return err
			}

			return playUsingHautomoClient(cmd.Context(), audioURL, "http://localhost:8084")
		},
	})

	cli.Execute(app)
}

func speakCLI(ctx context.Context, phrase string) error {
	if err := ErrorIfUnset(phrase == "", "phrase"); err != nil {
		return err
	}

	effects := defaultEffects()

	speechDownloadURL, err := effects.TextToSpeechURL(ctx, phrase)
	if err != nil {
		return err
	}

	slog.Debug("got URL", "url", speechDownloadURL)

	return playCLI(ctx, speechDownloadURL, effects)
}

func playCLI(ctx context.Context, audioURL string, effects Effects) error {
	if err := effects.PlayAudio(ctx, audioURL); err != nil {
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
