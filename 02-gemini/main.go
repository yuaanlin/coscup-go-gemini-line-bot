package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"
)

func main() {
	channelToken := os.Getenv("LINE_CHANNEL_TOKEN")
	channelSecret := os.Getenv("LINE_CHANNEL_SECRET")

	bot, err := messaging_api.NewMessagingApiAPI(channelToken)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/callback", func(w http.ResponseWriter, req *http.Request) {
		cb, err := webhook.ParseRequest(channelSecret, req)
		if err != nil {
			log.Printf("Cannot parse request: %+v\n", err)
			if errors.Is(err, webhook.ErrInvalidSignature) {
				w.WriteHeader(400)
			} else {
				w.WriteHeader(500)
			}
			return
		}

		for _, event := range cb.Events {
			switch e := event.(type) {
			case webhook.MessageEvent:
				switch e.Message.(type) {
				case webhook.TextMessageContent:
					handleTextMessage(bot, e)
				}
			}
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}

	fmt.Println("http://localhost:" + port + "/")
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func handleTextMessage(bot *messaging_api.MessagingApiAPI, e webhook.MessageEvent) {
	message := e.Message.(webhook.TextMessageContent)
	_, err := bot.ReplyMessage(&messaging_api.ReplyMessageRequest{ReplyToken: e.ReplyToken, Messages: []messaging_api.MessageInterface{messaging_api.TextMessage{Text: message.Text}}})
	if err != nil {
		log.Print(err)
	}
}
