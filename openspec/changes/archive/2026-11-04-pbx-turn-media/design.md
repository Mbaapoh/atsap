# Design — TURN for browser media

## D1. Configuration, not tenant data

TURN is installation-wide, like the SIP registrar and WebRTC URL. The
credential is static in Phase A and comes from the deployment secret
store. It is deliberately returned to signed-in tenant users: a browser
cannot allocate a relay without it. Rotating it is a deployment change.

## D2. The API stays the source

`GetSipRegistrar` already returns the browser's signalling address; it
now returns an ordered list of ICE servers in the shape
`RTCPeerConnection` takes. The console does not construct a STUN server
or guess a TURN URL.

## D3. Dev vs production

The dev compose runs coturn with host networking so relay candidates are
reachable from a browser on the same machine. Production runs coturn (or
another TURN service) with a public address, TLS where required, and a
credential from the secret store. The platform only hands out the URLs
and credential; it does not proxy media.

## D4. Fail open to host candidates

Empty `TURN_URLS` yields an empty ICE list, not an error. A browser
behind a normal NAT still connects; a symmetric NAT does not, and the
console says nothing false about it. The release checklist runs the
TURN UAT.
