// Command ouft-accounts is the OUFT accounts service (PocketBase app):
//
//	ouft-accounts serve --http 0.0.0.0:8090          run it (the compose setup does this)
//	ouft-accounts invite --uses 1 --days 7           print a new invite link
//	ouft-accounts superuser upsert EMAIL PASSWORD    create the admin for the dashboard (/_/)
//
// Environment: OUFT_PUBLIC_URL (invite links), OUFT_SERVICE_KEYS ("fut15:key,...").
package main

import (
	"log"

	"github.com/floholz/FIFA-15-Local-FUT-hosted/internal/accounts"
	"github.com/pocketbase/pocketbase"
)

func main() {
	app := pocketbase.New()
	accounts.Setup(app, accounts.ConfigFromEnv())
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
