FROM alpine:latest

ENTRYPOINT ["/usr/bin/home-audio"]

ADD rel/home-audio_linux-amd64 /usr/bin/home-audio
