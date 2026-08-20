package app

import (
	"fmt"

	"Go-Invoicing/internal/config"
	"Go-Invoicing/internal/handlers"
)

func Run() {
	cfg := config.Load()
	h := handlers.New(cfg.AppName)

	fmt.Println(h.WelcomeMessage())
}
