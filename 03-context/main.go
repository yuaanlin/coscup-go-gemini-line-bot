package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"
	"google.golang.org/api/option"
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

	reply, err := askGemini([]genai.Part{genai.Text(message.Text)})
	if err != nil {
		log.Fatal(err)
		return
	}

	_, err = bot.ReplyMessage(&messaging_api.ReplyMessageRequest{ReplyToken: e.ReplyToken, Messages: []messaging_api.MessageInterface{messaging_api.TextMessage{Text: reply}}})
	if err != nil {
		log.Print(err)
	}
}

func askGemini(data []genai.Part) (string, error) {
	ctx := context.Background()

	client, err := genai.NewClient(ctx, option.WithAPIKey(os.Getenv("GEMINI_API_KEY")))
	if err != nil {
		return "", fmt.Errorf("create genai client: %w", err)
	}

	defer client.Close()

	model := client.GenerativeModel("gemini-1.5-flash")
	cs := model.StartChat()

	res, err := cs.SendMessage(ctx, data...)
	if err != nil {
		return err.Error(), nil
	}

	resStr := ""
	for _, cand := range res.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				resStr = fmt.Sprintf("%s%s", resStr, part)
			}
		}
	}

	return strings.TrimSpace(resStr), nil
}
