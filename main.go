package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

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

var urlTemplate template.Template = *template.Must(template.ParseFiles("domain.html"))

type UrlData struct {
	Link string
}

func isValidUrl(url string, client *http.Client, receiver chan Result) {
	data := UrlData{Link: url}
	buf := &bytes.Buffer{}
	if err := urlTemplate.Execute(buf, data); err != nil {
		log.Fatal("Could not read template")
	}

	resp, err := client.Head(url)
	if err != nil {
		receiver <- Result{url: buf.String(), valid: false}
		return
	}
	statusCode := resp.StatusCode
	receiver <- Result{url: buf.String(), valid: statusCode >= 200 && statusCode <= 399}
}

func ask(siteName string) (chan Result, int) {
	content, err := os.ReadFile("tlds.txt")
	if err != nil {
		log.Fatal(err)
	}
	domains := strings.Split(string(content), "\n")

	scanAmount := 0
	scanReceiver := make(chan Result)
	client := http.Client{Timeout: time.Second * 5}
	for _, domain := range domains {
		url := "http://" + siteName + "." + strings.TrimSpace(domain)
		go isValidUrl(url, &client, scanReceiver)
		scanAmount++
	}

	return scanReceiver, scanAmount
}

func main() {
	http.HandleFunc("/ask", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Fatal(err)
		}

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
			receiver, amount := ask(siteName)
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
		tmpl, err := template.ParseFiles("index.html")
		if err != nil {
			log.Fatal(err)
		}
		tmpl.Execute(w, nil)
	})

	log.Fatal(http.ListenAndServe(":8100", nil))
}
