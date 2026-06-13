//go:build ignore

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

func main() {
	format := flag.String("format", "env", "output format: env or shell")
	flag.Parse()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	publicValue := base64.StdEncoding.EncodeToString(publicKey)
	privateValue := base64.StdEncoding.EncodeToString(privateKey)

	switch *format {
	case "env":
		fmt.Printf("MINDFS_RELEASE_PUBLIC_KEY=%s\n", publicValue)
		fmt.Printf("MINDFS_RELEASE_PRIVATE_KEY=%s\n", privateValue)
	case "shell":
		fmt.Printf("export MINDFS_RELEASE_PUBLIC_KEY=%q\n", publicValue)
		fmt.Printf("export MINDFS_RELEASE_PRIVATE_KEY=%q\n", privateValue)
	default:
		fmt.Fprintf(os.Stderr, "unsupported format: %s\n", *format)
		os.Exit(1)
	}
}
