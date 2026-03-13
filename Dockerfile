ARG GO_BUILDER_IMAGE=docker.ispider.io/golang:1.24
FROM ${GO_BUILDER_IMAGE} AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/control-plane ./cmd/control-plane

FROM scratch
ENV CONTROL_PLANE_LISTEN_ADDR=:8080
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/control-plane /control-plane
EXPOSE 8080
ENTRYPOINT ["/control-plane"]
