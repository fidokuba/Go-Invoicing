package app

import "github.com/jackc/pgx/v5/pgxpool"

type App struct {
	DB *pgxpool.Pool
}

func New(db *pgxpool.Pool) *App {
	return &App{
		DB: db,
	}
}