FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /grook ./cmd/grook

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /grook /grook
ENTRYPOINT ["/grook"]
