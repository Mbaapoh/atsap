# syntax=docker/dockerfile:1
#
# Builds Asterisk from source on Debian. ARI, AMI and chan_pjsip are all
# enabled by default in a standard build; --with-pjproject-bundled and
# --with-jansson-bundled make sure their dependencies (SIP stack, JSON)
# are available regardless of what's packaged for this Debian release.
#
# ASTERISK_VERSION is PINNED (never `-current`) and verified by SHA-256 at
# build time so two builds a week apart produce the same engine. The
# documented baseline is Asterisk 22.x LTS (docs/hld/README.md §2, TRD).
#
# To upgrade: change ASTERISK_VERSION and ASTERISK_SHA256 together, and
# verify the checksum first, e.g.:
#   curl -fL -O https://downloads.asterisk.org/pub/telephony/asterisk/releases/asterisk-<VERSION>.tar.gz
#   sha256sum asterisk-<VERSION>.tar.gz   # must equal ASTERISK_SHA256 below
# (the downloads.asterisk.org *.sha256 sidecars are intermittently 404;
# the asterisk/asterisk GitHub release page for the tag lists the digest.
# Older point releases live under releases/, the -current alias does not.)

FROM debian:bookworm-slim

ARG ASTERISK_VERSION=22.11.0
# Verified 2026-09-09 against the official releases/asterisk-22.11.0.tar.gz
# (26,724,408 bytes; sha256sum of a local download).
#
# DO NOT DOWNGRADE BELOW 22.11.0. This version is load-bearing for the
# e2e suite, not a routine currency bump.
#
# 22.11.0 carries "res_http_websocket: Unref session when it fails to
# establish" (#2020): two error paths leaked an ast_websocket session,
# one reachable when the ARI pre-callback fails. On 22.8.2 that leak
# wedges the engine after the first e2e run — see
# docs/ASTERISK-INTEGRATION.md §2.1.1 for the controlled A/B (identical
# test code and rig, only this ARG changed: 22.8.2 gave 1 pass then 5
# failures with threads climbing +4 per run; 22.11.0 gave 6 passes with
# threads flat).
#
# Not #1979. That issue (handshake read with no timeout, hanging a
# thread holding the channel lock) has the right shape but the wrong
# direction — it is the OUTBOUND client path, and nothing here makes
# Asterisk dial out over WebSocket. Recorded so it is not re-credited.
ARG ASTERISK_SHA256=3bd5ee040509a3d3cd9b1ba9520c18e6ec0a7e7981ca68c457dcd36ba3c54d94

RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential \
      wget \
      ca-certificates \
      subversion \
      git \
      pkg-config

WORKDIR /usr/src
RUN set -eux; \
    wget -q -O "asterisk-${ASTERISK_VERSION}.tar.gz" \
      "https://downloads.asterisk.org/pub/telephony/asterisk/releases/asterisk-${ASTERISK_VERSION}.tar.gz"; \
    echo "${ASTERISK_SHA256}  asterisk-${ASTERISK_VERSION}.tar.gz" | sha256sum -c -; \
    tar -xzf "asterisk-${ASTERISK_VERSION}.tar.gz"; \
    rm "asterisk-${ASTERISK_VERSION}.tar.gz"; \
    mv "asterisk-${ASTERISK_VERSION}" asterisk

WORKDIR /usr/src/asterisk
RUN contrib/scripts/install_prereq install -y \
    && rm -rf /var/lib/apt/lists/*

RUN ./configure --with-pjproject-bundled --with-jansson-bundled \
    && make -j"$(nproc)" \
    && make install \
    && make samples \
    && make config \
    && ldconfig

RUN rm -rf /usr/src/asterisk

RUN groupadd -r asterisk && useradd -r -g asterisk -d /var/lib/asterisk asterisk \
    && mkdir -p /var/run/asterisk \
    && chown -R asterisk:asterisk \
      /etc/asterisk \
      /var/lib/asterisk \
      /var/log/asterisk \
      /var/spool/asterisk \
      /var/run/asterisk \
      /usr/lib/asterisk

COPY conf/ /etc/asterisk/

EXPOSE 5060/udp 5060/tcp 8088/tcp 5038/tcp
EXPOSE 10000-10200/udp

USER asterisk
ENTRYPOINT ["/usr/sbin/asterisk"]
CMD ["-f", "-vvv"]
