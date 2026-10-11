// Command dev-licence makes the dev rig licensable without committing key
// material (pbx-call-placement design D8).
//
// The dev app boots in Setup — no call path — unless it is built trusting
// a development key and given a token that key signed (D-52, D-54). This
// creates (or reuses) a throwaway Ed25519 keypair under a gitignored
// directory and prints, as .env lines, the public key the dev image is
// built with and a token signed by it:
//
//	go run ./cmd/dev-licence -dir ../.dev-licence
//
// A production build is untouched: it is built without the public key, so
// it trusts only the vendor key and rejects this token.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	dir := flag.String("dir", ".dev-licence", "directory holding the development keypair (gitignored)")
	flag.Parse()

	lic, err := Generate(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dev-licence:", err)
		os.Exit(1)
	}
	fmt.Printf("ATSAPBX_DEV_LICENCE_PUBKEY=%s\n", lic.PublicKeyHex)
	fmt.Printf("ATSAPBX_LICENSE_TOKEN=%s\n", lic.Token)
}
