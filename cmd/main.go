package main

import (
	"net/http"
	"os"

	"github.com/allocup-2026-tabaru/untokosyo-be/internal/ws"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	_ = ws.NewHubManager()

	// サンプル: カウンターブロードキャスト用 Hub
	sampleHub := ws.NewHub()
	go sampleHub.Run()

	r := chi.NewRouter()
	r.Use(cors.AllowAll().Handler)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	// サンプル: カウンターブロードキャストの動作確認エンドポイント
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(sampleHub, w, r)
	})

	http.ListenAndServe(":"+port, r)
}
