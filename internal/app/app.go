package app

import (
	"fmt"

	"go-invoicing/internal/administration"
)

func Run() {
	fmt.Println("Welcome to Go Invoicing.")
	// Initialize app with admin config
	_ = administration.Organisation{}
}
