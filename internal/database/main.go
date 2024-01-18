package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"sort"
	"sync"
)

type DB struct {
	path string
	mux  *sync.RWMutex
}

type DBStructure struct {
	Chirps map[int]Chirp `json:"chirps"`
}

type Chirp struct {
	Body string `json:"body"`
	Id   int    `json:"id"`
}

// NewDB creates a new database connection
// and creates the database file if it doesn't exist
func NewDB(path string) (*DB, error) {
	db := DB{path, &sync.RWMutex{}}
	db.ensureDB()

	return &db, nil
}

// CreateChirp creates a new chirp and saves it to disk
func (db *DB) CreateChirp(body string) (Chirp, error) {
	dbContent, err := db.loadDB()
	if err != nil {
		return Chirp{}, err
	}

	id := len(dbContent.Chirps) + 1
	chirp := Chirp{body, id}
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

// GetChirps returns all chirps in the database
func (db *DB) GetChirps() ([]Chirp, error) {
	dbContent, err := db.loadDB()
	if err != nil {
		return make([]Chirp, 0), err
	}

	chirps := []Chirp{}
	for _, chirp := range dbContent.Chirps {
		chirps = append(chirps, chirp)
	}

	sort.Slice(chirps, func(i, j int) bool { return chirps[i].Id < chirps[j].Id })
	return chirps, nil
}

func (db *DB) GetChirpByID(id int) (Chirp, error) {
	dbContent, err := db.loadDB()
	if err != nil {
		return Chirp{"", -2}, err
	}

	chirp, ok := dbContent.Chirps[id]
	if !ok {
		return Chirp{"", -1}, nil
	}

	return chirp, nil
}

// ensureDB creates a new database file if it doesn't exist
func (db *DB) ensureDB() error {
	_, err := db.loadDB()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			data, _ := json.Marshal(DBStructure{make(map[int]Chirp)})
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
