FROM "golang:1.25-bookworm" AS build
ENV GO111MODULE=on
ENV CGO_ENABLED=0
ENV GOOS=linux

RUN mkdir /build
WORKDIR /build
COPY ./go.* /build
RUN --mount=type=cache,target=/root/.cache/go-build go mod download

COPY . /build
SHELL ["/bin/bash", "-c"]
RUN --mount=type=cache,target=/root/.cache/go-build go build -buildvcs=false ./cmd/sdafs
RUN --mount=type=cache,target=/root/.cache/go-build  go build -buildvcs=false ./cmd/csi-driver

USER 65534

FROM scratch
HEALTHCHECK NONE
COPY --from=build /build/sdafs /sdafs
COPY --from=build /build/csi-driver /csi-driver
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT [ "/csi-driver" ]
USER 65534
