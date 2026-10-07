FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/musiclub ./cmd/musiclub
RUN mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/musiclub /musiclub
# distroless has no shell, so create the data dir here, owned by the nonroot user
COPY --from=build --chown=nonroot:nonroot /out/data /data
VOLUME /data
ENV DB_PATH=/data/club.db PORT=8080 TZ=Europe/London
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/musiclub", "healthcheck"]
ENTRYPOINT ["/musiclub"]
