package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Result struct {
	url   string
	valid bool
}

func getCurDir() string {
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	exPath := filepath.Dir(ex)
	return exPath
}

var urlTemplate template.Template = *template.Must(template.ParseFiles(getCurDir() + "/domain.html"))

type UrlData struct {
	Link string
}

func isValidUrl(url string, client *http.Client, receiver chan Result, cache *ristretto.Cache) {
	data := UrlData{Link: url}
	buf := &bytes.Buffer{}
	if err := urlTemplate.Execute(buf, data); err != nil {
		log.Fatal("Could not read template")
	}

	cachedValue, found := cache.Get(url)
	if found {
		receiver <- Result{url: buf.String(), valid: cachedValue.(bool)}
		return
	}

	resp, err := client.Head(url)
	if err != nil {
		receiver <- Result{url: buf.String(), valid: false}
		cache.Set(url, false, 1)
		return
	}
	statusCode := resp.StatusCode
	isValidStatusCode := statusCode >= 200 && statusCode <= 399
	receiver <- Result{url: buf.String(), valid: isValidStatusCode}
	cache.Set(url, isValidStatusCode, 1)
}

func ask(siteName string, cache *ristretto.Cache) (chan Result, int) {
	if len(siteName) == 0 {
		return nil, 0
	}
	content, err := os.ReadFile(getCurDir() + "/tlds.txt")
	if err != nil {
		log.Fatal(err)
	}
	domains := strings.Split(string(content), "\n")

	scanAmount := 0
	scanReceiver := make(chan Result)
	client := http.Client{Timeout: time.Second * 5}
	for _, domain := range domains {
		url := "http://" + siteName + "." + strings.TrimSpace(domain)
		go isValidUrl(url, &client, scanReceiver, cache)
		scanAmount++
	}

	return scanReceiver, scanAmount
}

func main() {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,     // number of keys to track frequency of (10M).
		MaxCost:     1 << 30, // maximum cost of cache (1GB).
		BufferItems: 64,      // number of keys per Get buffer.
	})
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/ask", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("upgraded")

		handling := ""

		for {
			mt, c, err := conn.ReadMessage()
			if err != nil {
				log.Println(err)
				return
			}
			var data interface{}
			err = json.Unmarshal(c, &data)
			if err != nil {
				log.Println(err)
				return
			}
			siteName := data.(map[string]interface{})["site-name"].(string)
			if handling == siteName {
				continue
			}
			handling = siteName
			receiver, amount := ask(handling, cache)
			for amount > 0 {
				res := <-receiver
				if res.valid {
					err := conn.WriteMessage(mt, []byte(res.url))
					if err != nil {
						log.Fatal(err)
					}
				}
				amount--
			}
		}
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles(getCurDir() + "/index.html")
		if err != nil {
			log.Fatal(err)
		}
		tmpl.Execute(w, nil)
	})

	log.Fatal(http.ListenAndServe(":8100", nil))
}
