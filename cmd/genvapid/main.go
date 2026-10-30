// Команда genvapid печатает пару VAPID-ключей для .env (docs/06-reminders-push.md §4.6).
// Запуск: go run ./cmd/genvapid
package main

import (
	"fmt"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func main() {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		fmt.Fprintf(os.Stderr, "генерация VAPID-ключей: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("VAPID_PUBLIC_KEY=%s\n", publicKey)
	fmt.Printf("VAPID_PRIVATE_KEY=%s\n", privateKey)
	fmt.Println("VAPID_SUBJECT=mailto:demo@tracker.local")
}
