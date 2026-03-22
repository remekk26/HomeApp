FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN go build -o /homelab-dashboard main.go

FROM alpine:3.21
COPY --from=build /homelab-dashboard /homelab-dashboard
VOLUME /data
ENV DATA_FILE=/data/services.json
EXPOSE 3003
CMD ["/homelab-dashboard"]
