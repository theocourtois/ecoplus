FROM gcr.io/distroless/static:nonroot
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/hetzner-ecoplus-server /hetzner-ecoplus-server
USER nonroot:nonroot
ENTRYPOINT ["/hetzner-ecoplus-server"]
