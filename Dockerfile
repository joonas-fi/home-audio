FROM alpine:latest

ENTRYPOINT ["/usr/bin/home-audio"]

LABEL metrics.endpoint=:80/metrics

CMD ["server"]

ADD rel/home-audio_linux-amd64 /usr/bin/home-audio
