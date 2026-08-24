FROM golang:1.27.0-alpine3.23 AS build
WORKDIR /src
COPY go.mod ./
COPY e2e/exporter/main.go ./e2e/exporter/main.go
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tmr-exporter ./e2e/exporter

FROM scratch
COPY --from=build /out/tmr-exporter /tmr-exporter
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/tmr-exporter"]
