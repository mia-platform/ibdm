# syntax=docker/dockerfile:1
FROM docker.io/library/alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

ARG TARGETPLATFORM
ARG CMD_NAME
ENV COMMAND_NAME=${CMD_NAME}

# TODO: remove once the alpine base image ships zlib >= 1.3.2-r1
RUN apk upgrade --no-cache zlib

COPY ${TARGETPLATFORM}/${CMD_NAME} /usr/local/bin/

USER 1000:1000

CMD ["/bin/sh", "-c", "${COMMAND_NAME}"]
