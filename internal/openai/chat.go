package openai

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

var (
	client *openai.Client
	msgMap sync.Map
)

// Message 消息结构
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Init 初始化 OpenAI 客户端
func Init(apiKey, baseURL, engine string) {
	if apiKey == "" {
		log.Println("OpenAI: API key not configured")
		return
	}

	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}

	if engine != "" {
		// Azure OpenAI 配置
		cfg = openai.DefaultAzureConfig(apiKey, baseURL)
		cfg.AzureModelMapperFunc = func(model string) string {
			azureModelMapping := map[string]string{
				"gpt-3.5-turbo": engine,
				"gpt-4":        engine,
			}
			if m, ok := azureModelMapping[model]; ok {
				return m
			}
			return model
		}
	}

	client = openai.NewClientWithConfig(cfg)
	log.Println("OpenAI: Client initialized")
}

// Chat 发送聊天请求
func Chat(messages []openai.ChatCompletionMessage) (string, error) {
	if client == nil {
		return "", fmt.Errorf("OpenAI client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model:    openai.GPT3Dot5Turbo,
			Messages: messages,
		},
	)

	if err != nil {
		log.Printf("ChatCompletion error: %v", err)
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}

// SendMessage 发送消息（支持会话历史）
func SendMessage(userID string, question string, systemPrompt string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("OpenAI client not initialized")
	}

	var messages []openai.ChatCompletionMessage

	// 添加系统提示
	if systemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		})
	}

	// 检查是否有历史会话
	if val, ok := msgMap.Load(userID); ok {
		msgList := val.([]openai.ChatCompletionMessage)

		// 检查是否需要重置会话
		if question == "复位" || question == "结束会话" || question == "重新开始" || question == "reset" {
			msgMap.Delete(userID)
			return "会话已结束，请重新提问", nil
		}

		// 限制历史记录长度
		if len(msgList) > 10 {
			msgList = msgList[len(msgList)-10:]
		}

		messages = append(messages, msgList...)
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: question,
		})
	} else {
		// 新会话
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: question,
		})
	}

	// 保存会话历史
	msgMap.Store(userID, messages)

	// 调用 OpenAI
	response, err := Chat(messages)
	if err != nil {
		return "", err
	}

	// 保存助手的回复
	msgMap.Store(userID, append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: response,
	}))

	log.Printf("[OpenAI] User %s: %s\n\tAssistant: %s", userID, question, response)

	return response, nil
}

// ResetSession 重置会话
func ResetSession(userID string) {
	msgMap.Delete(userID)
}

// GetSessionHistory 获取会话历史
func GetSessionHistory(userID string) []openai.ChatCompletionMessage {
	if val, ok := msgMap.Load(userID); ok {
		return val.([]openai.ChatCompletionMessage)
	}
	return nil
}
