package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	"go.etcd.io/bbolt"
)

var (
	jsonOut  = flag.Bool("jsonout", false, "Output to json")
	dbfile   = flag.String("db", "nomenclator.db", "Database file")
	jsonFile = flag.String("json", "nomenclator.json", "JSON file")
	logFile  = flag.String("log", "nomenclator.log", "Log file")
	keyCount = flag.Int("keycount", 100, "Number of keys to generate")
	url      = flag.String("url", "http://fairlady:8080/", "nomenclator api url")
	// url      = flag.String("url", "http://localhost:8080/", "nomenclator api url")
)

type Application struct {
	JsonData []Pair
	InFlight int
	Complete int
	Count    int
	URL      string
	Logger   *log.Logger
	Client   *http.Client
	Memory   *sync.RWMutex
	DB       *bbolt.DB
	NameChan chan NameResponse
}

type NameResponse struct {
	Data string `json:"data"`
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

func NewApplication(url, logfile, dbfile string, count int) (*Application, error) {
	logger, err := newLogger(logfile)
	if err != nil {
		return nil, err
	}

	db, err := newDB(dbfile)
	if err != nil {
		return nil, err
	}
	namech := make(chan NameResponse, 100)

	return &Application{
		Count:    count,
		NameChan: namech,
		URL:      url,
		Logger:   logger,
		Client:   &http.Client{},
		Memory:   &sync.RWMutex{},
		DB:       db,
	}, nil
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

func (a *Application) GeKeyName() {
	req, err := http.NewRequest("GET", a.URL, nil)
	if err != nil {
		a.Logger.Println("Error creating request: ", err)
		return
	}
	resp, err := a.Client.Do(req)
	if err != nil {
		a.Logger.Println("Error sending request", err)
		return
	}
	defer resp.Body.Close()
	var name NameResponse
	if err := json.NewDecoder(resp.Body).Decode(&name); err != nil {
		a.Logger.Println("Error decoding response", err)
		return
	}
	a.Logger.Println("Received name: ", name.Data)
	a.NameChan <- name
}

type Pair struct {
	Name string `json:"name"`
	Key  []byte `json:"key"`
}

func (a *Application) PairAndSaveKey(name NameResponse) error {
	// name := <-a.NameChan
	key, err := MakeKey()
	if err != nil {
		a.Logger.Println("Error generating key: ", err)
		return err
	}
	a.AddPair(name.Data, key)
	if err := a.DB.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("keys"))
		if err != nil {
			return err
		}
		// this is called key but not really a key, it's a value
		// a.Logger.Println("Saving key: ", key, name.Data)
		return bucket.Put([]byte(name.Data), key)
	}); err != nil {
		a.Logger.Println("Error saving key: ", err)
		return err
	}
	return nil
}

func (a *Application) Run(stop chan struct{}) {
	for {
		select {
		case <-stop:
			a.Logger.Println("Shutting down")
			return
		default:
			if a.Complete == a.Count {
				stop <- struct{}{}
				return
			}
			a.InFlight++
			fmt.Println("InFlight: ", a.InFlight, "Complete: ", a.Complete, "Count: ", a.Count)
			go a.GeKeyName()
			name := <-a.NameChan
			a.PairAndSaveKey(name)
			a.Complete++
		}
	}
}
