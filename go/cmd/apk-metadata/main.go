// SPDX-License-Identifier: LGPL-3.0-only
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/thienvu18/kylok/go/provisioning"
)

// Inspect a signed APK through public provisioning APIs and emit metadata for host tooling.
func main() {
	path := flag.String("apk", "", "signed APK to verify and inspect")
	application := flag.Bool("application", false, "inspect an ordinary managed app instead of requiring DPC components")
	signer := flag.String("apksigner", "apksigner", "Android SDK apksigner executable")
	analyzer := flag.String("apkanalyzer", "apkanalyzer", "Android SDK apkanalyzer executable")
	flag.Parse()
	inspect := provisioning.Inspect
	if *application {
		inspect = provisioning.InspectApplication
	}
	apk, err := inspect(context.Background(), *path, provisioning.Tools{APKSigner: *signer, APKAnalyzer: *analyzer})
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(apk.Metadata()); err != nil {
		log.Fatal(err)
	}
}
