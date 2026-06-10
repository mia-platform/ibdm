# syntax=docker/dockerfile:1
FROM docker.io/library/alpine:3.24.0@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4

ARG TARGETPLATFORM
ARG CMD_NAME
ENV COMMAND_NAME=${CMD_NAME}

COPY ${TARGETPLATFORM}/${CMD_NAME} /usr/local/bin/

USER 1000:1000

CMD ["/bin/sh", "-c", "${COMMAND_NAME}"]
