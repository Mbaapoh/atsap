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

FROM debian:bookworm-slim AS builder

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

# The runtime image must not ship the compiler, Python or the -dev
# packages install_prereq pulls in (the image scan gap in D-86). Gather
# only the installed tree and the shared libraries the binary and every
# module actually link.
RUN set -eux; \
    mkdir -p /runtime/root; \
    { ldd /usr/sbin/asterisk; for m in /usr/lib/asterisk/modules/*.so; do ldd "$m" || true; done; } 2>/dev/null \
      | awk '/=> \// {print $3} /^\// {print $1}' | sort -u > /tmp/runtime-libs; \
    while read -r lib; do \
      [ -e "$lib" ] || continue; \
      case "$lib" in \
        /lib/*) dest="/usr/lib/${lib#/lib/}" ;; \
        /lib64/*) dest="/usr/lib64/${lib#/lib64/}" ;; \
        *) dest="$lib" ;; \
      esac; \
      mkdir -p "/runtime/root$(dirname "$dest")"; \
      cp -L "$lib" "/runtime/root$dest"; \
    done < /tmp/runtime-libs; \
    mkdir -p /runtime/root/usr/sbin /runtime/root/usr/lib /runtime/root/usr/share \
      /runtime/root/var/lib /runtime/root/var/spool /runtime/root/var/log /runtime/root/etc; \
    cp -a /usr/sbin/asterisk /runtime/root/usr/sbin/; \
    cp -a /usr/lib/asterisk /runtime/root/usr/lib/; \
    cp -a /usr/share/asterisk /runtime/root/usr/share/ 2>/dev/null || true; \
    cp -a /var/lib/asterisk /runtime/root/var/lib/; \
    cp -a /var/spool/asterisk /runtime/root/var/spool/; \
    cp -a /var/log/asterisk /runtime/root/var/log/; \
    cp -a /etc/asterisk /runtime/root/etc/; \
    if ls /usr/lib/x86_64-linux-gnu/libasterisk*.so >/dev/null 2>&1; then \
      mkdir -p /runtime/root/usr/lib/x86_64-linux-gnu; \
      cp -aL /usr/lib/x86_64-linux-gnu/libasterisk*.so /runtime/root/usr/lib/x86_64-linux-gnu/; \
    fi

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      libsrtp2-1 \
      libasound2 \
      apt \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /runtime/root/ /
RUN ldconfig \
    && groupadd -r asterisk && useradd -r -g asterisk -d /var/lib/asterisk asterisk \
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
