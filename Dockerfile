FROM golang as builder
RUN git clone https://github.com/ptracz/aquarea2mqtt.git /usr/local/go/src/aquarea2mqtt
RUN CGO_ENABLED=0 go build -C /usr/local/go/src/aquarea2mqtt -a -tags netgo -ldflags '-w -extldflags "-static"' -o /go/bin/aquarea2mqtt

FROM alpine
RUN adduser -S -D -H -h /aquarea appuser
USER appuser
COPY --from=builder /go/bin/aquarea2mqtt /aquarea/aquarea2mqtt
COPY --from=builder /usr/local/go/src/aquarea2mqtt/config.example.json /data/options.json
COPY --from=builder /usr/local/go/src/aquarea2mqtt/translation.json /aquarea/translation.json
WORKDIR /aquarea
ENTRYPOINT ./aquarea2mqtt
