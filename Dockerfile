FROM gcr.io/distroless/static:nonroot
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/ecoplus-server /ecoplus-server
USER nonroot:nonroot
ENTRYPOINT ["/ecoplus-server"]
