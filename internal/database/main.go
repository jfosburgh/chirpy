package database

import (
	"encoding/json"
	"errors"
	"fmt"

	// "fmt"
	"io/fs"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type DB struct {
	path string
	mux  *sync.RWMutex
}

type DBStructure struct {
	Chirps        map[int]Chirp        `json:"chirps"`
	Users         map[int]User         `json:"users"`
	RevokedTokens map[string]time.Time `json:"revoked_at"`
}

type Chirp struct {
	Body     string `json:"body"`
	Id       int    `json:"id"`
	AuthorId int    `json:"author_id"`
}

type User struct {
	EMail    string `json:"email"`
	Password []byte `json:"password"`
	Id       int    `json:"id"`
	IsRed    bool   `json:"is_chirpy_red"`
}

// NewDB creates a new database connection
// and creates the database file if it doesn't exist
func NewDB(path string) (*DB, error) {
	db := DB{path, &sync.RWMutex{}}
	db.ensureDB()

	return &db, nil
}

// CreateChirp creates a new chirp and saves it to disk
func (db *DB) CreateChirp(body string, author_id int) (Chirp, error) {
	dbContent, err := db.loadDB()
	if err != nil {
		return Chirp{}, err
	}

	id := len(dbContent.Chirps) + 1
	chirp := Chirp{body, id, author_id}
	if id == 1 {
		dbContent.Chirps = map[int]Chirp{}
	}
	dbContent.Chirps[id] = chirp
	err = db.writeDB(dbContent)
	if err != nil {
		return Chirp{}, err
	}

	return chirp, nil
}

func (db *DB) Login(email, password string) (User, error) {
	dbContent, err := db.loadDB()
	if err != nil {
		return User{}, err
	}

	for _, dbUser := range dbContent.Users {
		if dbUser.EMail == email {
			err = bcrypt.CompareHashAndPassword(dbUser.Password, []byte(password))
			if err != nil {
				return User{}, err
			} else {
				return dbUser, nil
			}
		}
	}

	return User{}, errors.New(fmt.Sprintf("No user found in db for email %s", email))
}

func (db *DB) TokenIsRevoked(token string) bool {
	dbContent, _ := db.loadDB()
	_, revoked := dbContent.RevokedTokens[token]
	return revoked
}

func (db *DB) RevokeToken(token string) error {
	dbContent, err := db.loadDB()
	if err != nil {
		return err
	}
	dbContent.RevokedTokens[token] = time.Now()
	db.writeDB(dbContent)
	return nil
}

func (db *DB) CreateUser(email, password string) (User, error) {
	dbContent, err := db.loadDB()
	if err != nil {
		return User{}, err
	}

	id := len(dbContent.Users) + 1
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	user := User{email, hashedPassword, id, false}
	if id == 1 {
		dbContent.Users = map[int]User{}
	}
	dbContent.Users[id] = user
	err = db.writeDB(dbContent)
	if err != nil {
		return User{}, err
	}

	return user, nil
}

func (db *DB) UpdateUser(id int, email, password string) (User, error) {
	dbContent, err := db.loadDB()
	if err != nil {
		return User{}, err
	}
	user, ok := dbContent.Users[id]
	if !ok {
		return User{}, errors.New("ID not found")
	}
	user.EMail = email
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	user.Password = hashedPassword
	dbContent.Users[id] = user
	db.writeDB(dbContent)

	return user, nil
}

func (db *DB) UpdateUserRedStatus(id int, red bool) error {
	dbContent, err := db.loadDB()
	if err != nil {
		return err
	}
	user, ok := dbContent.Users[id]
	if !ok {
		return errors.New("ID not found")
	}
	user.IsRed = red
	dbContent.Users[id] = user
	db.writeDB(dbContent)

	return nil
}

// GetChirps returns all chirps in the database
func (db *DB) GetChirps(author_id int, sortDir string) ([]Chirp, error) {
	dbContent, err := db.loadDB()
	if err != nil {
		return make([]Chirp, 0), err
	}

	chirps := []Chirp{}
	for _, chirp := range dbContent.Chirps {
		if author_id == 0 || author_id == chirp.AuthorId {
			chirps = append(chirps, chirp)
		}
	}

	if sortDir == "desc" {
		sort.Slice(chirps, func(i, j int) bool { return chirps[i].Id > chirps[j].Id })
	} else {
		sort.Slice(chirps, func(i, j int) bool { return chirps[i].Id < chirps[j].Id })
	}
	return chirps, nil
}

func (db *DB) DeleteChirp(id, author_id int) bool {
	dbContent, err := db.loadDB()
	if err != nil {
		return true
	}

	chirp, ok := dbContent.Chirps[id]
	if !ok {
		return true
	}

	if chirp.AuthorId != author_id {
		return false
	}

	delete(dbContent.Chirps, id)
	db.writeDB(dbContent)
	return true
}

func (db *DB) GetChirpByID(id int) (Chirp, error) {
	dbContent, err := db.loadDB()
	if err != nil {
		return Chirp{"", -2, -1}, err
	}

	chirp, ok := dbContent.Chirps[id]
	if !ok {
		return Chirp{"", -1, -1}, nil
	}

	return chirp, nil
}

// ensureDB creates a new database file if it doesn't exist
func (db *DB) ensureDB() error {
	_, err := db.loadDB()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			data, _ := json.Marshal(DBStructure{make(map[int]Chirp), make(map[int]User), make(map[string]time.Time)})
			err = os.WriteFile(db.path, data, 0666)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			log.Fatal(err)
		}
	}

	return err
}

// loadDB reads the database file into memory
func (db *DB) loadDB() (DBStructure, error) {
	data, err := os.ReadFile(db.path)
	if err != nil {
		return DBStructure{}, err
	}

	dbContent := DBStructure{}
	err = json.Unmarshal(data, &dbContent)

	return dbContent, nil
}

// writeDB writes the database file to disk
func (db *DB) writeDB(dbStructure DBStructure) error {
	data, err := json.Marshal(dbStructure)
	if err != nil {
		return err
	}

	err = os.WriteFile(db.path, data, 0666)
	if err != nil {
		return err
	}

	return nil
}
