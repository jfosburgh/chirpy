package main

import (
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"io/fs"
	"net/http"
	"os"
)

type apiConfig struct {
	fileserverHits int
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits++
		next.ServeHTTP(w, r)
	})
}

func middlewareCors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) metricResetHandler(w http.ResponseWriter, req *http.Request) {
	cfg.fileserverHits = 0
	w.WriteHeader(http.StatusOK)
}

func (cfg *apiConfig) metricHandler(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Add("Content-Type:", "text/html")
	page, _ := fs.ReadFile(os.DirFS("./"), "admin/index.html")
	w.Write([]byte(fmt.Sprintf(string(page), cfg.fileserverHits)))
}

func validateChirpHandler(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}

	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	type errorVals struct {
		Body string `json:"error"`
	}
	if err != nil {
		w.WriteHeader(500)
		respBody := errorVals{
			Body: "something went wrong",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}
	if len(params.Body) > 140 {
		w.WriteHeader(400)
		respBody := errorVals{
			Body: "chirp too long",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	type returnVals struct {
		Body bool `json:"valid"`
	}
	dat, _ := json.Marshal(returnVals{Body: true})
	w.WriteHeader(200)
	w.Write(dat)
}

func healthHandler(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(200)
	w.Header().Add("Content-Type:", "text/plain; charset=utf-8")
	w.Write([]byte("OK"))
}

func main() {
	r := chi.NewRouter()
	api := chi.NewRouter()
	admin := chi.NewRouter()
	corsMux := middlewareCors(r)

	cfg := apiConfig{0}
	r.Handle("/app", cfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	r.Handle("/app/*", cfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))

	api.Get("/healthz", healthHandler)
	api.Post("/validate_chirp", validateChirpHandler)
	admin.Get("/metrics", cfg.metricHandler)
	api.HandleFunc("/reset", cfg.metricResetHandler)
	r.Mount("/api", api)
	r.Mount("/admin", admin)

	server := http.Server{Addr: "localhost:8080", Handler: corsMux}
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Printf("ListenAndServe: %v", err)
	}
}
