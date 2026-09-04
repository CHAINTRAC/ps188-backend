# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
# Local document image storage (STORAGE_DRIVER=local, default LOCAL_STORAGE_DIR).
# Created here and copied with --chown below since distroless has no shell to
# mkdir/chown at runtime; a mounted named volume inherits this ownership on
# first creation.
RUN mkdir -p /out/data/uploads

# ---- runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build --chown=nonroot:nonroot /out/data /app/data

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
