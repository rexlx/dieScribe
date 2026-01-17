package main

import (
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

//go:embed data
var content embed.FS

var (
	jsonOut  = flag.Bool("jsonout", false, "Output to json")
	dbfile   = flag.String("db", "nomenclator.db", "Database file")
	jsonFile = flag.String("json", "nomenclator.json", "JSON file")
	logFile  = flag.String("log", "nomenclator.log", "Log file")
	keyCount = flag.Int("keycount", 100, "Number of keys to generate")
)

type Application struct {
	JsonData   []Pair
	InFlight   int
	Complete   int
	Count      int
	Logger     *log.Logger
	Memory     *sync.RWMutex
	DB         *bbolt.DB
	Adjectives []string
	Nouns      []string
	UsedNames  map[string]bool
}

type Pair struct {
	Name string `json:"name"`
	Key  []byte `json:"key"`
}

func (a *Application) AddPair(name string, key []byte) {
	a.Memory.Lock()
	defer a.Memory.Unlock()
	a.JsonData = append(a.JsonData, Pair{Name: name, Key: key})
}

func (a *Application) SaveJSON() error {
	a.Memory.RLock()
	defer a.Memory.RUnlock()
	file, err := os.OpenFile(*jsonFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	fmt.Println("Saving JSON", a.JsonData)
	return enc.Encode(a.JsonData)
}

func NewApplication(logfile, dbfile string, count int) (*Application, error) {
	logger, err := newLogger(logfile)
	if err != nil {
		return nil, err
	}

	db, err := newDB(dbfile)
	if err != nil {
		return nil, err
	}

	// Load words from the embedded filesystem
	adjs, err := loadWords("data/adj.txt")
	if err != nil {
		return nil, fmt.Errorf("failed to load adjectives: %v", err)
	}
	nouns, err := loadWords("data/noun.txt")
	if err != nil {
		return nil, fmt.Errorf("failed to load nouns: %v", err)
	}

	mrand.Seed(time.Now().UnixNano())

	return &Application{
		Count:      count,
		Logger:     logger,
		Memory:     &sync.RWMutex{},
		DB:         db,
		Adjectives: adjs,
		Nouns:      nouns,
		UsedNames:  make(map[string]bool),
	}, nil
}

func loadWords(path string) ([]string, error) {
	// Read from the embedded variable 'content' instead of os.ReadFile
	fileBytes, err := content.ReadFile(path)
	if err != nil {
		return nil, err
	}
	words := strings.Fields(string(fileBytes))

	var cleanWords []string
	for _, w := range words {
		if !strings.Contains(w, "[") && !strings.Contains(w, "]") {
			cleanWords = append(cleanWords, w)
		}
	}
	return cleanWords, nil
}

func MakeKey() ([]byte, error) {
	tmp := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, tmp); err != nil {
		return nil, err
	}
	return tmp, nil
}

func newLogger(logfile string) (*log.Logger, error) {
	file, err := os.OpenFile(logfile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}
	return log.New(file, "nomenclator: ", log.LstdFlags), nil
}

func newDB(dbfile string) (*bbolt.DB, error) {
	db, err := bbolt.Open(dbfile, 0600, nil)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (a *Application) GenerateUniqueName() (string, error) {
	maxRetries := 1000
	for i := 0; i < maxRetries; i++ {
		adj := a.Adjectives[mrand.Intn(len(a.Adjectives))]
		noun := a.Nouns[mrand.Intn(len(a.Nouns))]
		name := fmt.Sprintf("%s-%s", adj, noun)

		if !a.UsedNames[name] {
			a.UsedNames[name] = true
			return name, nil
		}
	}
	return "", errors.New("failed to generate unique name after many retries")
}

func (a *Application) PairAndSaveKey(name string) error {
	key, err := MakeKey()
	if err != nil {
		a.Logger.Println("Error generating key: ", err)
		return err
	}
	a.AddPair(name, key)
	if err := a.DB.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("keys"))
		if err != nil {
			return err
		}
		return bucket.Put([]byte(name), key)
	}); err != nil {
		a.Logger.Println("Error saving key: ", err)
		return err
	}
	return nil
}

func (a *Application) Run(stop chan struct{}) {
	a.Logger.Println("Generating names and keys...")

	totalCombos := len(a.Adjectives) * len(a.Nouns)
	if a.Count > totalCombos {
		fmt.Printf("Warning: Requested %d keys but only %d unique name combinations are possible.\n", a.Count, totalCombos)
	}

	for a.Complete < a.Count {
		select {
		case <-stop:
			a.Logger.Println("Shutting down")
			return
		default:
			name, err := a.GenerateUniqueName()
			if err != nil {
				a.Logger.Println("Generation stopped:", err)
				fmt.Println("Error:", err)
				stop <- struct{}{}
				return
			}
			a.Logger.Println("Generated name: ", name)

			if err := a.PairAndSaveKey(name); err != nil {
				a.Logger.Println("Error processing pair:", err)
				continue
			}

			a.Complete++
			fmt.Printf("Processed: %d / %d\r", a.Complete, a.Count)
		}
	}
	fmt.Println()
	stop <- struct{}{}
}
