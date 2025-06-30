package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"

	. "github.com/function61/gokit/builtin"
	"github.com/function61/gokit/encoding/jsonfile"
	"github.com/function61/gokit/net/http/httputils"
	"github.com/joonas-fi/home-audio/pkg/homeaudiotypes"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func server(ctx context.Context) error {
	srv := &http.Server{
		Addr:              ":" + FirstNonEmpty(os.Getenv("PORT"), "80"),
		Handler:           newServerHandler(defaultEffects()),
		ReadHeaderTimeout: httputils.DefaultReadHeaderTimeout,
	}

	return httputils.CancelableServer(ctx, srv, srv.ListenAndServe)
}

func defaultEffects() Effects {
	return Effects{
		TextToSpeech: makeSpeech,
		PlayAudio:    playUsingScreenServerClientScreenWall,
	}
}

type Effects struct {
	TextToSpeech func(ctx context.Context, phrase string) (string, error)
	PlayAudio    AudioPlayer
}

type AudioPlayer func(ctx context.Context, url string) error

func newServerHandler(effects Effects) http.Handler {
	routes := http.NewServeMux()

	routes.Handle("/metrics", promhttp.Handler())

	routes.HandleFunc("GET /home-audio/api/tts", httputils.WrapWithErrorHandling(func(w http.ResponseWriter, r *http.Request) error {
		phrase, err := enc.DecodeString(r.URL.Query().Get("phrase"))
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "audio/wav") // https://mimetype.io/audio/wav
		return wyomingTextToSpeech("192.168.1.105:10200", string(phrase), w)
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
		audioURL := makeAudioURL(req.Phrase)

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
	enc = base64.RawStdEncoding
)

func makeAudioURL(phrase string) string {
	return "http://192.168.1.105:5901/home-audio/api/tts?phrase=" + enc.EncodeToString([]byte(phrase))
}
