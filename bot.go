package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

func GetenvVar(key string, isEnvVarBase64 bool) string {
	value := os.Getenv(key)
	if !isEnvVarBase64 {
		return value
	}
	decodedValue, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		log.Fatal(err)
	}
	return string(decodedValue)
}

func DeleteMention(text string, entities []tgbotapi.MessageEntity) string {
	for _, entity := range entities {
		if entity.Type == "mention" {
			return text[:entity.Offset] + text[entity.Offset+entity.Length:]
		}
	}
	return text
}

func ConvertToTelegramHTML(text string) string {
	replacements := map[string]string{
		`## (.*)`:                            "<b>$1</b>",
		`\*\*(.*?)\*\*`:                      "<b>$1</b>",
		`__(.*?)__`:                          "<u>$1</u>",
		`\*(.*?)\*`:                          "<i>$1</i>",
		`_(.*?)_`:                            "<i>$1</i>",
		`\+\+(.*?)\+\+`:                      "<u>$1</u>",
		`~~(.*?)~~`:                          "<s>$1</s>",
		`\|\|(.*?)\|\|`:                      `<span class="tg-spoiler">$1</span>`,
		`\[(.*?)\]\((http[s]?:\/\/.*?)\)`:    `<a href="$2">$1</a>`,
		`\[(.*?)\]\(tg:\/\/user\?id=(\d+)\)`: `<a href="tg://user?id=$2">$1</a>`,
		"`([^`]+)`":                          "<code>$1</code>",
		"```([^`]*)```":                      "<pre>$1</pre>",
		`^> (.*)`:                            "<blockquote>$1</blockquote>",
	}

	for pattern, replacement := range replacements {
		re := regexp.MustCompile(pattern)
		if strings.Contains(pattern, "^") { // Handle multiline for blockquotes
			text = re.ReplaceAllStringFunc(text, func(s string) string {
				return re.ReplaceAllString(s, replacement)
			})
		} else {
			text = re.ReplaceAllString(text, replacement)
		}
	}

	return text
}

func Api(apiURL string, params map[string]interface{}) (map[string]interface{}, error) {
	jsonBody, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("error marshaling request body: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making API request: %w", err)
	}
	defer resp.Body.Close()

	var apiResponse map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&apiResponse)
	if err != nil {
		return nil, fmt.Errorf("error decoding API response: %w", err)
	}

	return apiResponse, nil
}

func handleStartCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update) error {
	START_TEXT := GetenvVar("START_TEXT", true)
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, START_TEXT)
	msg.ParseMode = tgbotapi.ModeMarkdown
	_, err := bot.Send(msg)
	return err
}

func handleInfoCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update, drugName string) error {
	info_text := drugName
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, info_text)
	msg.ParseMode = tgbotapi.ModeHTML
	_, err := bot.Send(msg)
	return err
}

func handleAskCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update, question string) error {
	// Group context and direct mention
	if update.Message.Chat.IsGroup() {
		mentioned := false
		for _, entity := range update.Message.Entities {
			if entity.Type == "mention" && entity.User.UserName == "doseslog_bot" {
				mentioned = true
				break
			}
		}
		if !mentioned {
			return nil // Exit if not mentioned in a group with an @mention
		}
	}

	// Typing indicator
	bot.Send(tgbotapi.NewChatAction(update.Message.Chat.ID, tgbotapi.ChatTyping))

	// Send "Thinking..." message
	thinkingMsg := tgbotapi.NewMessage(update.Message.Chat.ID, "PsyAI is thinking...")
	thinkingMsg.ReplyToMessageID = update.Message.MessageID // Reply to the original message
	thinkingMsgSent, err := bot.Send(thinkingMsg)
	if err != nil {
		return err
	}

	apiURL := GetenvVar("BASE_URL_BETA", false) + "/prompt?model=openai"
	question = DeleteMention(question, update.Message.Entities)
	requestBody := map[string]interface{}{
		"question":    question,
		"temperature": 0.25,
		"tokens":      1000,
	}

	apiResponse, err := Api(apiURL, requestBody)
	if err != nil {
		return err
	}

	answer, ok := apiResponse["assistant"].(string)
	answer = ConvertToTelegramHTML(answer)
	if !ok {
		return fmt.Errorf("unexpected API response format")
	}

	answerMsg := tgbotapi.NewEditMessageText(update.Message.Chat.ID, thinkingMsgSent.MessageID, answer)
	answerMsg.ParseMode = tgbotapi.ModeHTML
	_, err = bot.Send(answerMsg)
	return err
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")

	}

	// Constants
	TELETOKEN := GetenvVar("TELETOKEN", false)

	bot, err := tgbotapi.NewBotAPI(TELETOKEN)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60

	updates := bot.GetUpdatesChan(updateConfig)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		var err error

		switch update.Message.Command() {
		case "start":
			err = handleStartCommand(bot, update)
		case "info":
			drugName := update.Message.CommandArguments()
			log.Print(drugName)
			err = handleInfoCommand(bot, update, drugName)
		default:
			question := update.Message.Text
			err = handleAskCommand(bot, update, question)
		}

		if err != nil {
			log.Printf("Error handling command '%s': %v", update.Message.Command(), err)
		}
	}
}
