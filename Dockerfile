# syntax=docker/dockerfile:1
FROM docker.io/library/alpine:3.24.2@sha256:31b6477333eb8257db9e5d7c3a7264fd0467928756f0bbcc27d35bea5d28cdbd

ARG TARGETPLATFORM
ARG CMD_NAME
ENV COMMAND_NAME=${CMD_NAME}

COPY ${TARGETPLATFORM}/${CMD_NAME} /usr/local/bin/

USER 1000:1000

CMD ["/bin/sh", "-c", "${COMMAND_NAME}"]
