package main

import (
	"chirpy/internal/database"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type apiConfig struct {
	fileserverHits int
	db             *database.DB
}

type errorVals struct {
	Body string `json:"error"`
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

func cleanText(text string) string {
	words := strings.Split(text, " ")
	cleanedWords := make([]string, 0)

	for _, word := range words {
		if slices.Contains([]string{"kerfuffle", "sharbert", "fornax"}, strings.ToLower(word)) {
			cleanedWords = append(cleanedWords, "****")
		} else {
			cleanedWords = append(cleanedWords, word)
		}
	}

	cleanedText := strings.Join(cleanedWords, " ")
	return cleanedText
}

func (cfg *apiConfig) createUserHandler(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Email string `json:"email"`
	}

	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		w.WriteHeader(500)
		respBody := errorVals{
			Body: "something went wrong",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	user, err := cfg.db.CreateUser(params.Email)
	if err != nil {
		w.WriteHeader(500)
		respBody := errorVals{
			Body: string(fmt.Sprint(err)),
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	dat, _ := json.Marshal(user)
	w.WriteHeader(201)
	w.Write(dat)
}

func (cfg *apiConfig) createChirpHandler(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}

	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
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

	cleanedBody := cleanText(params.Body)
	chirp, err := cfg.db.CreateChirp(cleanedBody)
	if err != nil {
		w.WriteHeader(500)
		respBody := errorVals{
			Body: string(fmt.Sprint(err)),
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	dat, _ := json.Marshal(chirp)
	w.WriteHeader(201)
	w.Write(dat)
}

func (cfg *apiConfig) getChirpsHandler(w http.ResponseWriter, req *http.Request) {
	chirps, err := cfg.db.GetChirps()
	if err != nil {
		w.WriteHeader(500)
		respBody := errorVals{
			Body: string(fmt.Sprint(err)),
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	dat, _ := json.Marshal(chirps)
	w.WriteHeader(200)
	w.Header().Add("Content-Type:", "application/json")
	w.Write(dat)
}

func (cfg *apiConfig) getChirpByIDHandler(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(req, "id"))
	chirp, err := cfg.db.GetChirpByID(id)
	if err != nil || chirp.Id == -1 {
		if err == nil {
			w.WriteHeader(404)
		} else {
			w.WriteHeader(500)
		}
		respBody := errorVals{
			Body: string(fmt.Sprint(err)),
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	dat, _ := json.Marshal(chirp)
	w.WriteHeader(200)
	w.Header().Add("Content-Type:", "application/json")
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

	dbg := flag.Bool("debug", false, "Enable debug mode")
	flag.Parse()

	dbf := "/home/jfosburgh/workspace/github.com/jfosburgh/boot.dev/chirpy/db.json"
	if *dbg {
		os.Remove(dbf)
	}

	db, err := database.NewDB(dbf)
	if err != nil {
		log.Fatal(err)
	}

	cfg := apiConfig{0, db}
	r.Handle("/app", cfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	r.Handle("/app/*", cfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))

	api.Get("/healthz", healthHandler)
	api.Post("/chirps", cfg.createChirpHandler)
	api.Get("/chirps", cfg.getChirpsHandler)
	api.Get("/chirps/{id}", cfg.getChirpByIDHandler)
	api.Post("/users", cfg.createUserHandler)
	admin.Get("/metrics", cfg.metricHandler)
	api.HandleFunc("/reset", cfg.metricResetHandler)
	r.Mount("/api", api)
	r.Mount("/admin", admin)

	server := http.Server{Addr: "localhost:8080", Handler: corsMux}
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Printf("ListenAndServe: %v", err)
	}
}
