# ArgosFX backend — multi-stage, static binary, distroless final image.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/argosfx ./cmd/argosfx

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/argosfx /argosfx
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/argosfx"]
