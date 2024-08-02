package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"
	"go.mongodb.org/mongo-driver/bson"
	"google.golang.org/api/option"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	channelToken := os.Getenv("LINE_CHANNEL_TOKEN")
	channelSecret := os.Getenv("LINE_CHANNEL_SECRET")

	bot, err := messaging_api.NewMessagingApiAPI(channelToken)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	db := client.Database("bot")

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
					handleTextMessage(db, bot, e)
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

func handleTextMessage(db *mongo.Database, bot *messaging_api.MessagingApiAPI, e webhook.MessageEvent) {
	message := e.Message.(webhook.TextMessageContent)

	var data []genai.Part

	data = append(data, genai.Text("你是一個秘書，你的工作是根據和我的聊天記錄回答使用者的提問。"))
	data = append(data, genai.Text(fmt.Sprintf("在現在這個當下的時間是 %s，星期 %s", time.Now().Format("2006-01-02 15:04:05"), time.Now().Weekday().String())))
	data = append(data, genai.Text("以下是過去的聊天記錄："))

	msgs, err := db.Collection("messages").Find(context.Background(), bson.M{"userID": e.Source.(webhook.UserSource).UserId}, options.Find().SetSort(bson.M{"createdAt": -1}))
	if err == nil {
		defer msgs.Close(context.Background())
		for msgs.Next(context.Background()) {
			var msg bson.M
			if err := msgs.Decode(&msg); err != nil {
				log.Println(err)
				data = append(data, genai.Text("system: user sent a message but failed to decode"))
				continue
			}
			switch msg["type"].(string) {
			case "text":
				data = append(data, genai.Text(msg["role"].(string)+": "+msg["text"].(string)))
			case "image":
				data = append(data, genai.Text(msg["role"].(string)+": "+msg["content"].(string)))
			}
		}
	}

	_, err = db.Collection("messages").InsertOne(context.Background(), bson.M{
		"userID":    e.Source.(webhook.UserSource).UserId,
		"role":      "user",
		"type":      "text",
		"text":      message.Text,
		"createdAt": time.Now(),
	})
	if err != nil {
		log.Fatal(err)
		return
	}

	data = append(data, genai.Text("根據以上的聊天記錄，使用者現在和你說："))
	data = append(data, genai.Text(message.Text))
	data = append(data, genai.Text("請給他一個適當的回應，不要使用 markdown 語法，語氣盡量自然一點。"))
	data = append(data, genai.Text("注意：使用者的提問或許和聊天記錄有關，也可能無關，你需要自行判斷。"))
	data = append(data, genai.Text("注意：作為一個秘書，盡量避免和使用者無意義的延伸話題。"))
	data = append(data, genai.Text("注意：直接回覆使用者的回覆，而不要用「根據我們的對話紀錄，我應該回答...」這樣的方式。"))
	data = append(data, genai.Text("注意：當使用者詢問關於照片的問題時，你看到的是已經轉換為文字描述的照片內容，但你應該把他當做一張「照片」而不是「照片描述」，因為對使用者而言他覺得自己上傳了一張照片。"))
	data = append(data, genai.Text("注意：為了看起來更自然，結尾不需要句號或多餘的表情符號"))
	data = append(data, genai.Text("注意：如果他問關於「這個」的東西，指的是對話紀錄中最後的幾個部分，因為紀錄是按照時間排序的，所以「這個」指的是最近發給你的東西。"))

	start := time.Now()
	reply, err := askGemini(data)
	if err != nil {
		log.Fatal(err)
		return
	}
	log.Printf("Gemini took %v to reply, context length %d", time.Since(start), len(data))

	_, err = bot.ReplyMessage(&messaging_api.ReplyMessageRequest{ReplyToken: e.ReplyToken, Messages: []messaging_api.MessageInterface{messaging_api.TextMessage{Text: reply}}})
	if err != nil {
		log.Print(err)
	}

	_, err = db.Collection("messages").InsertOne(context.Background(), bson.M{
		"userID":    e.Source.(webhook.UserSource).UserId,
		"role":      "bot",
		"type":      "text",
		"text":      reply,
		"createdAt": time.Now(),
	})
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
