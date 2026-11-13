// verifysig checks a release's SHA256SUMS.sig with the signing keys built
// into SecureSpeak (internal/update/signing-keys.pem), using the same code
// the app uses before installing an update. CI runs it after signing.
//
//	go run ./tools/verifysig release/SHA256SUMS release/SHA256SUMS.sig
//	go run ./tools/verifysig -keys      # exit 0 if keys are built in, 1 if not
package main

import (
	"fmt"
	"os"

	"securespeak/internal/update"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-keys" {
		fmt.Printf("%d signing key(s) built in\n", len(update.TrustedKeys))
		if len(update.TrustedKeys) == 0 {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: verifysig SHA256SUMS SHA256SUMS.sig | -keys")
		os.Exit(2)
	}
	sums, err := os.ReadFile(os.Args[1])
	if err == nil {
		var sig []byte
		if sig, err = os.ReadFile(os.Args[2]); err == nil {
			err = update.VerifySums(sums, sig, update.TrustedKeys)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "signature check FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("signature OK")
}
