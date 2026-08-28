#!/usr/bin/env bash
# Source this before running anything in learn/:
#   source learn/env.sh
#
# Same credentials as .env.example, but pointed at localhost instead of the
# "asterisk" hostname — that name only resolves *inside* the docker compose
# network. Your Go programs run on the host, so they need localhost.
export ARI_URL=http://localhost:8088/ari
export ARI_USERNAME=voipapp
export ARI_PASSWORD=devpassword123
export ARI_APP_NAME=voip-app

export AMI_ADDR=localhost:5038
export AMI_USERNAME=voipapp
export AMI_PASSWORD=devpassword123
