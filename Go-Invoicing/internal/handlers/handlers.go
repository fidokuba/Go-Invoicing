package handlers

import "fmt"

type Handler struct {
	AppName string
}

func New(appName string) Handler {
	return Handler{AppName: appName}
}

func (h Handler) WelcomeMessage() string {
	return fmt.Sprintf("Welcome to %s!", h.AppName)
}
