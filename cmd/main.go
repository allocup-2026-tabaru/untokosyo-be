package main

import (
	"context"
	"net/http"
	"os"

	"github.com/allocup-2026-tabaru/untokosyo-be/internal/api"
	"github.com/allocup-2026-tabaru/untokosyo-be/internal/domain"
	"github.com/allocup-2026-tabaru/untokosyo-be/internal/store"
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

	ctx := context.Background()

	roomStore := store.NewMemoryRoomStore()
	manager := ws.NewHubManager()
	wsHandler := ws.NewHandler(roomStore, manager)

	judge := domain.NewExtractionJudge(domain.JudgeConfig{
		Type:   domain.JudgeTypeSigmoid,
		Params: map[string]float64{"midpoint": 100.0, "steepness": 0.05},
	})
	roomHandler := api.NewRoomHandler(ctx, roomStore, manager, judge)

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

	r.Post("/rooms", roomHandler.Create)
	r.Get("/rooms/{roomID}", roomHandler.Get)
	r.Post("/rooms/{roomID}/players", roomHandler.Join)
	r.Post("/rooms/{roomID}/start", roomHandler.Start)
	r.Delete("/rooms/{roomID}", roomHandler.Delete)

	r.Get("/ws/rooms/{roomID}/host", wsHandler.ServeHostWS)
	r.Get("/ws/rooms/{roomID}/player", wsHandler.ServePlayerWS)

	http.ListenAndServe(":"+port, r)
}
