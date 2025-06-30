package main

import (
	"cmp"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/function61/gokit/encoding/jsonfile"
	"github.com/function61/gokit/net/http/httputils"
	"github.com/joonas-fi/home-audio/internal/ttscache"
	"github.com/joonas-fi/home-audio/pkg/homeaudiotypes"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func server(ctx context.Context) error {
	srv := &http.Server{
		Addr:              ":" + cmp.Or(os.Getenv("PORT"), "80"),
		Handler:           newServerHandler(defaultEffects()),
		ReadHeaderTimeout: httputils.DefaultReadHeaderTimeout,
	}

	return httputils.CancelableServer(ctx, srv, srv.ListenAndServe)
}

func defaultEffects() Effects {
	return Effects{
		TextToSpeechURL: makeSpeechURLHomeAudio,
		TextToSpeech: func(ctx context.Context, phrase string, writer io.Writer) error {
			const homeFn61NetPiper = "192.168.1.105:10200"
			return wyomingTextToSpeech(ctx, homeFn61NetPiper, phrase, writer)
		},
		// TextToSpeech:    makeSpeechHomeAssistant,
		PlayAudio: playUsingScreenServerClientScreenWall,
	}
}

type Effects struct {
	TextToSpeechURL func(ctx context.Context, phrase string) (string, error)
	// outputs phrase as audio (.wav) bytes
	TextToSpeech func(ctx context.Context, phrase string, writer io.Writer) error
	PlayAudio    AudioPlayer
}

// given URL with audio, plays it via speaker(s)
type AudioPlayer func(ctx context.Context, url string) error

func newServerHandler(effects Effects) http.Handler {
	cache := ttscache.New()

	routes := http.NewServeMux()

	routes.Handle("/metrics", promhttp.Handler())

	routes.HandleFunc("GET /home-audio/api/tts", httputils.WrapWithErrorHandling(func(w http.ResponseWriter, r *http.Request) error {
		phrase, err := base64RawStd.DecodeString(r.URL.Query().Get("phrase"))
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "audio/wav") // https://mimetype.io/audio/wav

		// FIXME: use timer instead of work-based trigger?
		cache.PurgeOldItems()

		// this is only ran if the item is not in cache
		ttsPhraseGenerator := func(sink io.WriteCloser) error {
			if err := effects.TextToSpeech(r.Context(), string(phrase), sink); err != nil {
				return err
			}

			return sink.Close()
		}

		if _, err := io.Copy(w, cache.Get(string(phrase), ttsPhraseGenerator)); err != nil {
			return err
		}

		return nil
	}))

	routes.HandleFunc("POST /home-audio/api/speak", httputils.WrapWithErrorHandling(func(w http.ResponseWriter, r *http.Request) error {
		req := homeaudiotypes.SpeakInput{}
		if err := jsonfile.UnmarshalDisallowUnknownFields(r.Body, &req); err != nil {
			return err
		}

		// audio, err := effects.TextToSpeech(r.Context(), req.Phrase)
		// if err != nil {
		// 	return fmt.Errorf("TextToSpeech: %w", err)
		// }
		audioURL, err := effects.TextToSpeechURL(r.Context(), req.Phrase)
		if err != nil {
			return err
		}

		playOne := func(p AudioPlayer) error {
			return p(r.Context(), audioURL)
		}

		if len(req.Devices) == 0 {
			return playOne(effects.PlayAudio)
		}

		for _, deviceID := range req.Devices {
			device, found := deviceRegistry[deviceID]
			if !found {
				return fmt.Errorf("device not found: %s", deviceID)
			}

			if err := playOne(device); err != nil {
				return fmt.Errorf("playing to device '%s': %w", deviceID, err)
			}
		}

		return nil
	}))

	return routes
}

var deviceRegistry = map[string]AudioPlayer{
	"screen-wall": playUsingScreenServerClientScreenWall,
	"work":        playUsingWorkHautomoClient,
}

var (
	base64RawStd = base64.RawStdEncoding
)

// uses home-audio to drive text-to-speech process
func makeSpeechURLHomeAudio(_ context.Context, phrase string) (string, error) {
	return "http://192.168.1.105:5901/home-audio/api/tts?phrase=" + base64RawStd.EncodeToString([]byte(phrase)), nil
}
