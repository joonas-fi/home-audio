package main

import (
	"context"
	"io"

	. "github.com/function61/gokit/builtin"
	"github.com/function61/gokit/net/http/ezhttp"
	"github.com/function61/gokit/os/osutil"
)

// uses Home Assistant to drive text-to-speech process
//
//nolint:unused
func makeSpeechHomeAssistant(ctx context.Context, phrase string, sink io.Writer) error {
	withErr := FuncWrapErr

	if err := ErrorIfUnset(phrase == "", "phrase"); err != nil {
		return withErr(err)
	}

	baseURL := "https://home-assistant.home.fn61.net"
	// used to use a proxy which was exposed in firewall
	// baseURL := "http://home.fn61.net:5901"
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

	speechDownloadURL := baseURL + res.Path

	audioResp, err := ezhttp.Get(ctx, speechDownloadURL)
	if err != nil {
		return withErr(err)
	}
	defer audioResp.Body.Close()

	if _, err := io.Copy(sink, audioResp.Body); err != nil {
		return withErr(err)
	}
	return nil
}
