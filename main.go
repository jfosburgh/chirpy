package main

import (
	"chirpy/internal/database"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
)

type apiConfig struct {
	fileserverHits int
	db             *database.DB
	jwtSecret      string
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

type userparameters struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userNoPassword struct {
	Email string `json:"email"`
	Id    int    `json:"id"`
}

type userWithToken struct {
	Email        string `json:"email"`
	Id           int    `json:"id"`
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

func (cfg *apiConfig) loginHandler(w http.ResponseWriter, req *http.Request) {
	decoder := json.NewDecoder(req.Body)
	params := userparameters{}
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

	user, err := cfg.db.Login(params.Email, params.Password)

	if err != nil {
		w.WriteHeader(401)
		respBody := errorVals{
			Body: "Unauthorized",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	now := time.Now().UTC()
	expireAccess := now.Add(time.Millisecond * 1000 * 60 * 60)
	expireRefresh := now.Add(time.Millisecond * 1000 * 60 * 60 * 24)
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.RegisteredClaims{Issuer: "chirpy-access", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expireAccess), Subject: fmt.Sprint(user.Id)})
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.RegisteredClaims{Issuer: "chirpy-refresh", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expireRefresh), Subject: fmt.Sprint(user.Id)})
	signedAccessToken, err := accessToken.SignedString([]byte(cfg.jwtSecret))
	signedRefreshToken, err := refreshToken.SignedString([]byte(cfg.jwtSecret))

	returnUser := userWithToken{
		Email:        user.EMail,
		Id:           user.Id,
		Token:        signedAccessToken,
		RefreshToken: signedRefreshToken,
	}

	dat, _ := json.Marshal(returnUser)
	w.WriteHeader(200)
	w.Write(dat)
}

func (cfg *apiConfig) revokeTokenHandler(w http.ResponseWriter, req *http.Request) {
	token, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok {
		w.WriteHeader(401)
		respBody := errorVals{
			Body: "authorization token required",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	cfg.db.RevokeToken(token)
}

func (cfg *apiConfig) refreshTokenHandler(w http.ResponseWriter, req *http.Request) {
	token, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok {
		w.WriteHeader(401)
		respBody := errorVals{
			Body: "authorization token required",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}
	tokenPtr, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(cfg.jwtSecret), nil
	})
	if err != nil {
		w.WriteHeader(401)
		respBody := errorVals{
			Body: "Unauthorized",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	if issuer, _ := tokenPtr.Claims.GetIssuer(); issuer != "chirpy-refresh" {
		w.WriteHeader(401)
		return
	}
	id, _ := tokenPtr.Claims.GetSubject()

	if cfg.db.TokenIsRevoked(token) {
		w.WriteHeader(401)
		return
	}

	now := time.Now()
	expireAccess := now.Add(time.Millisecond * 1000 * 60 * 60)
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.RegisteredClaims{Issuer: "chirpy-access", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expireAccess), Subject: fmt.Sprint(id)})
	signedAccessToken, err := accessToken.SignedString([]byte(cfg.jwtSecret))

	type accesstoken struct {
		Token string `json:"token"`
	}
	dat, _ := json.Marshal(accesstoken{signedAccessToken})
	w.Write(dat)
}

func (cfg *apiConfig) parseJWT(w http.ResponseWriter, req *http.Request, issuer string) (*jwt.Token, error) {
	token, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok {
		w.WriteHeader(401)
		respBody := errorVals{
			Body: "authorization token required",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return &jwt.Token{}, errors.New("No auth token")
	}
	tokenPtr, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(cfg.jwtSecret), nil
	})
	if err != nil {
		w.WriteHeader(401)
		respBody := errorVals{
			Body: "Unauthorized",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return &jwt.Token{}, err
	}

	if issuer, _ := tokenPtr.Claims.GetIssuer(); issuer != issuer {
		w.WriteHeader(401)
		return &jwt.Token{}, errors.New("Incorrect issuer")
	}

	return tokenPtr, nil
}

func (cfg *apiConfig) updateUserHandler(w http.ResponseWriter, req *http.Request) {
	tokenPtr, err := cfg.parseJWT(w, req, "chirpy-access")
	if err != nil {
		return
	}

	idString, _ := tokenPtr.Claims.GetSubject()
	id, _ := strconv.Atoi(idString)

	decoder := json.NewDecoder(req.Body)
	params := userparameters{}
	err = decoder.Decode(&params)
	if err != nil {
		w.WriteHeader(500)
		respBody := errorVals{
			Body: "something went wrong",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	user, err := cfg.db.UpdateUser(id, params.Email, params.Password)
	if err != nil {
		w.WriteHeader(500)
		respBody := errorVals{
			Body: "something went wrong",
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	type returnuser struct {
		Email string `json:"email"`
		Id    int    `json:"id"`
	}
	returnUser := returnuser{user.EMail, user.Id}
	dat, _ := json.Marshal(returnUser)
	w.Write(dat)
}

func (cfg *apiConfig) createUserHandler(w http.ResponseWriter, req *http.Request) {
	decoder := json.NewDecoder(req.Body)
	params := userparameters{}
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

	user, err := cfg.db.CreateUser(params.Email, params.Password)

	if err != nil {
		w.WriteHeader(500)
		respBody := errorVals{
			Body: string(fmt.Sprint(err)),
		}
		dat, _ := json.Marshal(respBody)
		w.Write(dat)
		return
	}

	returnUser := userNoPassword{
		Email: user.EMail,
		Id:    user.Id,
	}

	dat, _ := json.Marshal(returnUser)
	w.WriteHeader(201)
	w.Write(dat)
}

func (cfg *apiConfig) createChirpHandler(w http.ResponseWriter, req *http.Request) {
	jwtToken, err := cfg.parseJWT(w, req, "chirpy-access")
	if err != nil {
		return
	}

	strId, _ := jwtToken.Claims.GetSubject()
	id, _ := strconv.Atoi(strId)

	type parameters struct {
		Body string `json:"body"`
	}

	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err = decoder.Decode(&params)
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
	chirp, err := cfg.db.CreateChirp(cleanedBody, id)
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

func (cfg *apiConfig) deleteChirpHandler(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(req, "id"))
	jwtToken, err := cfg.parseJWT(w, req, "chirpy-access")
	if err != nil {
		w.WriteHeader(401)
		return
	}
	strId, err := jwtToken.Claims.GetSubject()
	author_id, err := strconv.Atoi(strId)

	ok := cfg.db.DeleteChirp(id, author_id)
	if !ok {
		w.WriteHeader(403)
		return
	}
	return
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
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env")
	}
	jwtSecret := os.Getenv("JWT_SECRET")
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

	cfg := apiConfig{0, db, jwtSecret}
	r.Handle("/app", cfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	r.Handle("/app/*", cfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))

	api.Get("/healthz", healthHandler)
	api.Post("/chirps", cfg.createChirpHandler)
	api.Get("/chirps", cfg.getChirpsHandler)
	api.Get("/chirps/{id}", cfg.getChirpByIDHandler)
	api.Delete("/chirps/{id}", cfg.deleteChirpHandler)
	api.Post("/users", cfg.createUserHandler)
	api.Post("/login", cfg.loginHandler)
	api.Put("/users", cfg.updateUserHandler)
	api.Post("/refresh", cfg.refreshTokenHandler)
	api.Post("/revoke", cfg.revokeTokenHandler)
	admin.Get("/metrics", cfg.metricHandler)
	api.HandleFunc("/reset", cfg.metricResetHandler)
	r.Mount("/api", api)
	r.Mount("/admin", admin)

	server := http.Server{Addr: "localhost:8080", Handler: corsMux}
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Printf("ListenAndServe: %v", err)
	}
}
