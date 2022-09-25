package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	redpanda "github.com/segmentio/kafka-go"
)

func main() {

	router := chi.NewRouter()
	User(router)
	Sawer(router)
	Notification(router)
	fs := http.FileServer(http.Dir(`public`))
	router.Handle(`/public/`, fs)
	f, _ := os.OpenFile(`public/index.html`, os.O_RDONLY, os.ModeType)
	index, _ := io.ReadAll(f)
	router.Get(`/`, func(w http.ResponseWriter, r *http.Request) {
		w.Write(index)
	})
	go Bank()
	fmt.Println(http.ListenAndServe(`:6090`, router))
}

type user string
type amount uint64

var bank map[user]amount = make(map[user]amount)

type UserBankBalance struct {
	User   string `json:"user"`
	Amount uint64 `json:"amount"`
}

var RedPandaHost = `localhost:9092`

func User(router *chi.Mux) {
	// type Login_In struct {
	// 	Username string `json:"username"`
	// }
	router.Route(`/login/{username}`, func(r chi.Router) {
		r.Get(`/`, func(w http.ResponseWriter, r *http.Request) {

			username := chi.URLParam(r, `username`)
			if username == `` {
				w.WriteHeader(400)
				w.Write([]byte(`bad request`))
				return
			}
			http.SetCookie(w, &http.Cookie{Name: `username`, Value: username, Expires: time.Now().Add(1 * time.Hour), HttpOnly: true, Path: `/`, SameSite: http.SameSiteDefaultMode})
			w.Header().Add(`location`, `http://localhost:6090/`)
			w.WriteHeader(302)

		})
	})

	router.Get(`/balance`, func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(`username`)
		if err == http.ErrNoCookie {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`login first`))
			return
		}
		out := UserBankBalance{
			User: ck.Value,
		}
		if v, ok := bank[user(ck.Value)]; ok {
			out.Amount = uint64(v)
		}
		if err := json.NewEncoder(w).Encode(out); err != nil {
			w.WriteHeader(500)
			w.Write([]byte(err.Error()))
			return
		}

	})
}

// RedPanda stuff
type RedPandaMsgKey []byte

var SawerMessage RedPandaMsgKey = []byte(`sawer`)

type SawerRedpandaMessageSchema struct {
	FromUsername        string `json:"from"`
	DestinationUsername string `json:"destination"`
	Amount              uint64 `json:"amount"`
	Message             string `json:"message"`
}

func notificationTopic(username string) string {
	return `notification` + username
}

func bankTopic() string {
	return `bank`
}

// Banking stuff
func Bank() {
	//i want to count all money from redpanda because i still have no persistent storage
	reader := redpanda.NewReader(redpanda.ReaderConfig{
		Brokers: []string{RedPandaHost},
		Topic:   bankTopic(),
		// GroupID:     `1`,
		// StartOffset: redpanda.FirstOffset,
	})
read:
	for {
		msg, err := reader.ReadMessage(context.Background())
		if err != nil {
			log.Println(err)
			reader = redpanda.NewReader(redpanda.ReaderConfig{
				Brokers: []string{RedPandaHost},
				Topic:   bankTopic(),
				GroupID: `1`,
			})
			continue read
		}
		switch string(msg.Key) {
		case string(SawerMessage):
			sawer := SawerRedpandaMessageSchema{}
			if err := json.Unmarshal(msg.Value, &sawer); err != nil {
				log.Println(`bank sawer: `, err)
				continue read
			}
			bank[user(sawer.DestinationUsername)] += amount(sawer.Amount)
		}
	}
}

// Sawer stuff
func Sawer(router *chi.Mux) {
	type Sawer_In struct {
		FromUsername        string `json:"-"`
		DestinationUsername string `json:"destination"`
		Amount              uint64 `json:"amount"`
		Message             string `json:"message"`
	}

	router.Post(`/sawer`, func(w http.ResponseWriter, r *http.Request) {

		ck, err := r.Cookie(`username`)
		if err == http.ErrNoCookie {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`login first`))
			return
		}
		in := Sawer_In{FromUsername: ck.Value}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			w.WriteHeader(400)
			w.Write([]byte(err.Error()))
			return
		}
		rpWr := &redpanda.Writer{
			Addr: redpanda.TCP(RedPandaHost),

			Balancer:               &redpanda.LeastBytes{},
			AllowAutoTopicCreation: true,
		}
		// b := make([]byte, 8)
		// sender := ``
		// binary.LittleEndian.PutUint64(b, in.Amount)

		sawerMsg := SawerRedpandaMessageSchema{
			FromUsername:        in.FromUsername,
			DestinationUsername: in.DestinationUsername,
			Amount:              in.Amount,
			Message:             in.Message,
		}
		b, err := json.Marshal(sawerMsg)
		if err != nil {
			w.WriteHeader(500)
			w.Write([]byte(err.Error()))
			fmt.Println(err)
			return
		}
		if err := rpWr.WriteMessages(context.Background(), redpanda.Message{
			Key:   []byte(`sawer`),
			Topic: notificationTopic(in.DestinationUsername),
			Value: b,
		}, redpanda.Message{
			Key:   []byte(`sawer`),
			Topic: bankTopic(),
			Value: b,
		},
		); err != nil {
			w.WriteHeader(500)
			w.Write([]byte(err.Error()))
			fmt.Println(err)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`ok`))
	})
}

// Notification
type NotificationSawerScheme_Out struct {
	From    string `json:"from"`
	Amount  uint64 `json:"amount"`
	Message string `json:"message"`
}

func Notification(router *chi.Mux) {
	router.Get(`/notification`, func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(`username`)
		if err == http.ErrNoCookie {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`login first`))
			return
		}
		//set header for sse
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		flusher, ok := w.(http.Flusher)
		if !ok {
			w.Write([]byte(`sse not supported`))
			return
		}

		reader := redpanda.NewReader(redpanda.ReaderConfig{
			Brokers: []string{RedPandaHost},
			Topic:   notificationTopic(ck.Value),
			GroupID: `1`,
		})
		rpMsgChan := make(chan redpanda.Message, 2)
		rpErrChan := make(chan error)
		defer reader.Close()
		ctx, cancel := context.WithCancel(context.Background())
		go func(msgChan chan<- redpanda.Message, errChan chan<- error) {
			for {
				msg, err := reader.FetchMessage(ctx)
				if err != nil {
					// todo

					if err == context.Canceled {
						fmt.Println(err)
						close(msgChan)
						close(errChan)
						break
					}
					errChan <- err
				}
				msgChan <- msg
			}
		}(rpMsgChan, rpErrChan)
		byt := []byte{}
		id := 1

		for {
			select {

			case <-r.Context().Done():
				cancel()
				//clearing, actually no need wg
				{
					wg := sync.WaitGroup{}
					wg.Add(2)
					go func(msgChan chan redpanda.Message, wait *sync.WaitGroup) {
						for range msgChan {

						}
						wg.Done()
					}(rpMsgChan, &wg)
					go func(errChan chan error, wait *sync.WaitGroup) {
						for range errChan {
						}
						wg.Done()
					}(rpErrChan, &wg)
					wg.Wait()
				}

				log.Println(`connection closed`)
				return
			case msg := <-rpMsgChan:
				// buffer := bytes.NewBuffer(byt)
				// buffer.WriteString(fmt.Sprintf("id: %d\n", id))
				// buffer.WriteString("event: message\n")
				// buffer.WriteString(`data: `)
				// buffer.Write(msg.Value)
				// buffer.WriteByte('\n')
				// w.Write(byt)
				fmt.Fprintf(w, "data: %s\n\n", msg.Value)
				flusher.Flush()
				reader.CommitMessages(context.Background(), msg)
				id++
				byt = byt[:0]
			}

		}
	})
}
