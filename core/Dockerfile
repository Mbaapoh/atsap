# syntax=docker/dockerfile:1
#
# Builds Asterisk from source on Debian. ARI, AMI and chan_pjsip are all
# enabled by default in a standard build; --with-pjproject-bundled and
# --with-jansson-bundled make sure their dependencies (SIP stack, JSON)
# are available regardless of what's packaged for this Debian release.
#
# ASTERISK_MAJOR tracks the documented media engine baseline (22.x LTS —
# see docs/hld/README.md §2 and docs/TRD.md).

FROM debian:bookworm-slim

ARG ASTERISK_MAJOR=22

RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential \
      wget \
      ca-certificates \
      subversion \
      git \
      pkg-config

WORKDIR /usr/src
RUN wget -q "https://downloads.asterisk.org/pub/telephony/asterisk/asterisk-${ASTERISK_MAJOR}-current.tar.gz" \
    && tar zxf "asterisk-${ASTERISK_MAJOR}-current.tar.gz" \
    && rm "asterisk-${ASTERISK_MAJOR}-current.tar.gz" \
    && mv asterisk-${ASTERISK_MAJOR}.* asterisk

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
