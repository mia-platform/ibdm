# syntax=docker/dockerfile:1
FROM docker.io/library/alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

ARG TARGETPLATFORM
ARG CMD_NAME
ENV COMMAND_NAME=${CMD_NAME}

COPY ${TARGETPLATFORM}/${CMD_NAME} /usr/local/bin/

USER 1000:1000

CMD ["/bin/sh", "-c", "${COMMAND_NAME}"]
