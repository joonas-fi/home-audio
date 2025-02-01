package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	. "github.com/function61/gokit/builtin"
	"github.com/function61/gokit/encoding/jsonfile"
	"github.com/function61/gokit/net/http/httputils"
	"github.com/joonas-fi/home-audio/pkg/homeaudiotypes"
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
	PlayAudio    func(ctx context.Context, url string) error
}

func newServerHandler(effects Effects) http.Handler {
	routes := http.NewServeMux()

	routes.HandleFunc("POST /home-audio/api/speak", httputils.WrapWithErrorHandling(func(w http.ResponseWriter, r *http.Request) error {
		req := homeaudiotypes.SpeakInput{}
		if err := jsonfile.UnmarshalDisallowUnknownFields(r.Body, &req); err != nil {
			return err
		}

		audio, err := effects.TextToSpeech(r.Context(), req.Phrase)
		if err != nil {
			return fmt.Errorf("TextToSpeech: %w", err)
		}

		if err := effects.PlayAudio(r.Context(), audio); err != nil {
			return fmt.Errorf("PlayAudio: %w", err)
		}

		return nil
	}))

	return routes
}
