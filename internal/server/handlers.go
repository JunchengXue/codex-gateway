package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeCodexModels(w)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request", "failed to read request body")
		return
	}

	var chatReq chatCompletionRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request", "invalid chat completion request json")
		return
	}

	codexReq, err := toCodexResponsesRequest(chatReq)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	codexBody, err := json.Marshal(codexReq)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request", "failed to encode upstream request")
		return
	}

	resp, err := s.deps.UpstreamClient.Do(
		r.Context(), http.MethodPost, s.deps.ResponsesPath,
		codexBody, "application/json",
		map[string]string{"originator": s.deps.Originator},
	)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_unavailable", "upstream request failed")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		relayUpstreamResponse(w, resp)
		return
	}

	if chatReq.Stream {
		s.streamCodexAsChatCompletions(w, resp.Body, chatReq.Model)
		return
	}

	s.writeCodexAsChatCompletionJSON(w, resp.Body, chatReq.Model)
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request", "failed to read request body")
		return
	}

	if len(strings.TrimSpace(string(body))) > 0 && !json.Valid(body) {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request", "invalid request body json")
		return
	}

	codexBody, err := normalizeCodexResponsesRequest(body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request", "invalid request body json")
		return
	}

	resp, err := s.deps.UpstreamClient.Do(
		r.Context(), http.MethodPost, s.deps.ResponsesPath,
		codexBody, r.Header.Get("Content-Type"),
		map[string]string{"originator": s.deps.Originator},
	)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_unavailable", "upstream request failed")
		return
	}

	relayUpstreamResponse(w, resp)
}

// --- request normalization ---

func normalizeCodexResponsesRequest(body []byte) ([]byte, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return body, nil
	}

	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, err
	}

	mutated := false

	for _, key := range []string{"max_output_tokens", "max_completion_tokens"} {
		if _, ok := obj[key]; ok {
			delete(obj, key)
			mutated = true
		}
	}

	if !hasCodexInstructions(obj["instructions"]) {
		obj["instructions"] = "You are a helpful assistant."
		mutated = true
	}

	if !mutated {
		return body, nil
	}

	return json.Marshal(obj)
}

func hasCodexInstructions(value any) bool {
	v, ok := value.(string)
	return ok && strings.TrimSpace(v) != ""
}

// --- chat completions → codex adapter types ---

type chatCompletionRequest struct {
	Model               string           `json:"model"`
	Messages            []chatMessage    `json:"messages"`
	Stream              bool             `json:"stream"`
	Temperature         *float64         `json:"temperature,omitempty"`
	TopP                *float64         `json:"top_p,omitempty"`
	MaxTokens           *int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int             `json:"max_completion_tokens,omitempty"`
	ToolChoice          any              `json:"tool_choice,omitempty"`
	Tools               []map[string]any `json:"tools,omitempty"`
	ParallelToolCalls   *bool            `json:"parallel_tool_calls,omitempty"`
	ReasoningEffort     string           `json:"reasoning_effort,omitempty"`
	Functions           []map[string]any `json:"functions,omitempty"`
	FunctionCall        any              `json:"function_call,omitempty"`
}

type chatMessage struct {
	Role         string            `json:"role"`
	Content      any               `json:"content,omitempty"`
	Name         string            `json:"name,omitempty"`
	ToolCallID   string            `json:"tool_call_id,omitempty"`
	ToolCalls    []chatToolCall    `json:"tool_calls,omitempty"`
	FunctionCall *chatFunctionCall `json:"function_call,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

type codexResponsesRequest struct {
	Model             string           `json:"model"`
	Instructions      string           `json:"instructions"`
	Input             []map[string]any `json:"input"`
	Store             bool             `json:"store"`
	Stream            bool             `json:"stream"`
	Temperature       *float64         `json:"temperature,omitempty"`
	TopP              *float64         `json:"top_p,omitempty"`
	Tools             []map[string]any `json:"tools,omitempty"`
	ToolChoice        any              `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool            `json:"parallel_tool_calls,omitempty"`
	Reasoning         *codexReasoning  `json:"reasoning,omitempty"`
}

type codexReasoning struct {
	Effort string `json:"effort,omitempty"`
}

// --- chat completions → codex conversion ---

func toCodexResponsesRequest(in chatCompletionRequest) (codexResponsesRequest, error) {
	model := strings.TrimSpace(in.Model)
	if model == "" {
		return codexResponsesRequest{}, fmt.Errorf("model is required")
	}

	var instructions []string
	var input []map[string]any
	legacyCallCount := 0

	for i, msg := range in.Messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			continue
		}

		switch role {
		case "system":
			if text := stringifyContent(msg.Content); text != "" {
				instructions = append(instructions, text)
			}
		case "tool":
			toolCallID := strings.TrimSpace(msg.ToolCallID)
			if toolCallID == "" {
				return codexResponsesRequest{}, fmt.Errorf("tool message requires tool_call_id")
			}
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": toolCallID,
				"output":  toCodexFunctionCallOutput(msg.Content),
			})
		case "assistant":
			callItems, err := toCodexAssistantToolCallItems(msg, i, &legacyCallCount)
			if err != nil {
				return codexResponsesRequest{}, err
			}
			input = append(input, callItems...)

			if msg.Content != nil || strings.TrimSpace(msg.Name) != "" {
				item := map[string]any{"role": "assistant"}
				if msg.Content != nil {
					item["content"] = msg.Content
				}
				if msg.Name != "" {
					item["name"] = msg.Name
				}
				input = append(input, item)
			}
		default:
			item := map[string]any{"role": role}
			if msg.Content != nil {
				item["content"] = msg.Content
			}
			if msg.Name != "" {
				item["name"] = msg.Name
			}
			input = append(input, item)
		}
	}

	if len(input) == 0 {
		return codexResponsesRequest{}, fmt.Errorf("at least one non-system message is required")
	}

	instructionText := strings.TrimSpace(strings.Join(instructions, "\n\n"))
	if instructionText == "" {
		instructionText = "You are a helpful assistant."
	}

	tools, err := toCodexTools(in.Tools, in.Functions)
	if err != nil {
		return codexResponsesRequest{}, err
	}

	toolChoice, err := toCodexToolChoice(in.ToolChoice, in.FunctionCall)
	if err != nil {
		return codexResponsesRequest{}, err
	}

	return codexResponsesRequest{
		Model:             model,
		Instructions:      instructionText,
		Input:             input,
		Store:             false,
		Stream:            true,
		Temperature:       in.Temperature,
		TopP:              in.TopP,
		Tools:             tools,
		ToolChoice:        toolChoice,
		ParallelToolCalls: in.ParallelToolCalls,
		Reasoning:         toCodexReasoning(in.ReasoningEffort),
	}, nil
}

func toCodexAssistantToolCallItems(msg chatMessage, messageIndex int, legacyCallCount *int) ([]map[string]any, error) {
	var items []map[string]any

	for i, call := range msg.ToolCalls {
		callType := strings.TrimSpace(call.Type)
		if callType == "" {
			callType = "function"
		}
		if callType != "function" {
			continue
		}

		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			return nil, fmt.Errorf("assistant tool_calls[%d].function.name is required", i)
		}

		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			callID = fmt.Sprintf("call_m%d_t%d", messageIndex, i)
		}

		arguments := call.Function.Arguments
		if strings.TrimSpace(arguments) == "" {
			arguments = "{}"
		}

		items = append(items, map[string]any{
			"type": "function_call", "call_id": callID,
			"name": name, "arguments": arguments,
		})
	}

	if msg.FunctionCall == nil {
		return items, nil
	}

	name := strings.TrimSpace(msg.FunctionCall.Name)
	if name == "" {
		return nil, fmt.Errorf("assistant function_call.name is required")
	}

	arguments := msg.FunctionCall.Arguments
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}

	*legacyCallCount++
	items = append(items, map[string]any{
		"type": "function_call", "call_id": fmt.Sprintf("call_legacy_%d", *legacyCallCount),
		"name": name, "arguments": arguments,
	})

	return items, nil
}

func toCodexFunctionCallOutput(content any) any {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		return v
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(encoded)
	}
}

func toCodexTools(chatTools []map[string]any, legacyFunctions []map[string]any) ([]map[string]any, error) {
	if len(chatTools) == 0 && len(legacyFunctions) > 0 {
		chatTools = make([]map[string]any, 0, len(legacyFunctions))
		for i, fn := range legacyFunctions {
			name := strings.TrimSpace(asString(fn["name"]))
			if name == "" {
				return nil, fmt.Errorf("functions[%d].name is required", i)
			}
			toolFn := map[string]any{"name": name}
			if desc, ok := fn["description"]; ok {
				toolFn["description"] = desc
			}
			if params, ok := fn["parameters"]; ok {
				toolFn["parameters"] = params
			}
			chatTools = append(chatTools, map[string]any{"type": "function", "function": toolFn})
		}
	}

	if len(chatTools) == 0 {
		return nil, nil
	}

	tools := make([]map[string]any, 0, len(chatTools))
	for i, tool := range chatTools {
		toolType := strings.TrimSpace(asString(tool["type"]))
		if toolType == "" {
			return nil, fmt.Errorf("tools[%d].type is required", i)
		}
		if toolType != "function" {
			tools = append(tools, tool)
			continue
		}

		rawFn, ok := tool["function"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tools[%d].function is required for function tools", i)
		}
		name := strings.TrimSpace(asString(rawFn["name"]))
		if name == "" {
			return nil, fmt.Errorf("tools[%d].function.name is required", i)
		}

		mapped := map[string]any{"type": "function", "name": name}
		if desc, ok := rawFn["description"]; ok {
			mapped["description"] = desc
		}
		if params, ok := rawFn["parameters"]; ok {
			mapped["parameters"] = params
		} else {
			mapped["parameters"] = nil
		}
		if strict, ok := rawFn["strict"]; ok {
			mapped["strict"] = strict
		} else {
			mapped["strict"] = false
		}
		tools = append(tools, mapped)
	}

	return tools, nil
}

func toCodexToolChoice(toolChoice any, legacyFunctionCall any) (any, error) {
	choice := toolChoice
	if choice == nil {
		choice = legacyFunctionCall
	}
	if choice == nil {
		return nil, nil
	}

	switch v := choice.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		return v, nil
	case map[string]any:
		if fn, ok := v["function"].(map[string]any); ok {
			name := strings.TrimSpace(asString(fn["name"]))
			if name == "" {
				return nil, fmt.Errorf("tool_choice.function.name is required")
			}
			return map[string]any{"type": "function", "name": name}, nil
		}
		if strings.TrimSpace(asString(v["type"])) == "function" {
			name := strings.TrimSpace(asString(v["name"]))
			if name == "" {
				return nil, fmt.Errorf("tool_choice.name is required for type=function")
			}
			return map[string]any{"type": "function", "name": name}, nil
		}
		if name := strings.TrimSpace(asString(v["name"])); name != "" {
			return map[string]any{"type": "function", "name": name}, nil
		}
		return v, nil
	default:
		return choice, nil
	}
}

func toCodexReasoning(effort string) *codexReasoning {
	if strings.TrimSpace(effort) == "" {
		return nil
	}
	return &codexReasoning{Effort: strings.TrimSpace(effort)}
}

func asString(value any) string {
	s, _ := value.(string)
	return s
}

func stringifyContent(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, strings.TrimSpace(text))
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// --- models ---

func writeCodexModels(w http.ResponseWriter) {
	type model struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	out := struct {
		Object string  `json:"object"`
		Data   []model `json:"data"`
	}{
		Object: "list",
		Data: []model{
			{ID: "gpt-5.3-codex", Object: "model", OwnedBy: "openai"},
			{ID: "gpt-5.2-codex", Object: "model", OwnedBy: "openai"},
			{ID: "gpt-5.1-codex", Object: "model", OwnedBy: "openai"},
			{ID: "gpt-5.1-codex-mini", Object: "model", OwnedBy: "openai"},
			{ID: "gpt-5.1-codex-max", Object: "model", OwnedBy: "openai"},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// --- SSE parsing ---

type sseEvent struct {
	Event string
	Data  string
}

func parseSSE(reader io.Reader, onEvent func(sseEvent) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	currentEvent := ""
	var dataLines []string

	emit := func() error {
		if len(dataLines) == 0 {
			currentEvent = ""
			return nil
		}
		err := onEvent(sseEvent{Event: currentEvent, Data: strings.Join(dataLines, "\n")})
		currentEvent = ""
		dataLines = nil
		return err
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := emit(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return emit()
}

// --- codex SSE event types ---

type codexResponseCreatedEvent struct {
	Response struct {
		ID        string `json:"id"`
		CreatedAt int64  `json:"created_at"`
		Model     string `json:"model"`
	} `json:"response"`
}

type codexResponseOutputTextDeltaEvent struct {
	Delta string `json:"delta"`
}

type codexResponseOutputItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type codexResponseOutputItemEvent struct {
	OutputIndex int                     `json:"output_index"`
	Item        codexResponseOutputItem `json:"item"`
}

type codexResponseFunctionCallArgumentsDeltaEvent struct {
	ItemID      string `json:"item_id"`
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

type codexResponseFunctionCallArgumentsDoneEvent struct {
	ItemID      string `json:"item_id"`
	OutputIndex int    `json:"output_index"`
	Name        string `json:"name"`
	Arguments   string `json:"arguments"`
}

type codexResponseCompletedEvent struct {
	Response struct {
		ID        string `json:"id"`
		CreatedAt int64  `json:"created_at"`
		Model     string `json:"model"`
		Usage     struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	} `json:"response"`
}

// --- tool call state tracking ---

type codexToolCallState struct {
	OutputIndex      int
	ToolIndex        int
	ItemID           string
	CallID           string
	Name             string
	Arguments        string
	MetadataSent     bool
	BufferedArgs     strings.Builder
	ArgumentsEmitted bool
}

func getOrCreateToolCallState(byOutputIndex map[int]*codexToolCallState, byItemID map[string]*codexToolCallState, ordered *[]*codexToolCallState, outputIndex int, itemID string) *codexToolCallState {
	trimmedID := strings.TrimSpace(itemID)
	if trimmedID != "" {
		if state, ok := byItemID[trimmedID]; ok {
			if _, exists := byOutputIndex[outputIndex]; !exists {
				byOutputIndex[outputIndex] = state
			}
			return state
		}
	}

	if state, ok := byOutputIndex[outputIndex]; ok {
		if trimmedID != "" {
			state.ItemID = trimmedID
			byItemID[trimmedID] = state
		}
		return state
	}

	state := &codexToolCallState{
		OutputIndex: outputIndex,
		ToolIndex:   len(*ordered),
		ItemID:      trimmedID,
	}
	byOutputIndex[outputIndex] = state
	if trimmedID != "" {
		byItemID[trimmedID] = state
	}
	*ordered = append(*ordered, state)
	return state
}

func applyOutputItemToState(state *codexToolCallState, item codexResponseOutputItem) {
	if id := strings.TrimSpace(item.ID); id != "" {
		state.ItemID = id
	}
	if callID := strings.TrimSpace(item.CallID); callID != "" {
		state.CallID = callID
	}
	if name := strings.TrimSpace(item.Name); name != "" {
		state.Name = name
	}
	if strings.TrimSpace(state.Arguments) == "" && item.Arguments != "" {
		state.Arguments = item.Arguments
	}
}

func buildChatToolCalls(states []*codexToolCallState) []map[string]any {
	if len(states) == 0 {
		return nil
	}

	out := make([]map[string]any, 0, len(states))
	for i, s := range states {
		toolID := strings.TrimSpace(s.CallID)
		if toolID == "" {
			toolID = strings.TrimSpace(s.ItemID)
		}
		if toolID == "" {
			toolID = fmt.Sprintf("call_%d", i+1)
		}

		name := strings.TrimSpace(s.Name)
		if name == "" {
			name = "unknown_function"
		}

		args := s.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}

		out = append(out, map[string]any{
			"id": toolID, "type": "function",
			"function": map[string]any{"name": name, "arguments": args},
		})
	}
	return out
}

// --- codex → chat completion response adapters ---

func (s *Server) writeCodexAsChatCompletionJSON(w http.ResponseWriter, body io.Reader, requestedModel string) {
	created := codexResponseCreatedEvent{}
	completed := codexResponseCompletedEvent{}
	var textBuilder strings.Builder
	byOutputIndex := map[int]*codexToolCallState{}
	byItemID := map[string]*codexToolCallState{}
	var orderedToolCalls []*codexToolCallState

	err := parseSSE(body, func(ev sseEvent) error {
		switch ev.Event {
		case "response.created":
			_ = json.Unmarshal([]byte(ev.Data), &created)
		case "response.output_text.delta":
			var delta codexResponseOutputTextDeltaEvent
			if json.Unmarshal([]byte(ev.Data), &delta) == nil {
				textBuilder.WriteString(delta.Delta)
			}
		case "response.output_item.added", "response.output_item.done":
			var itemEv codexResponseOutputItemEvent
			if json.Unmarshal([]byte(ev.Data), &itemEv) != nil || itemEv.Item.Type != "function_call" {
				return nil
			}
			state := getOrCreateToolCallState(byOutputIndex, byItemID, &orderedToolCalls, itemEv.OutputIndex, itemEv.Item.ID)
			applyOutputItemToState(state, itemEv.Item)
		case "response.function_call_arguments.delta":
			var d codexResponseFunctionCallArgumentsDeltaEvent
			if json.Unmarshal([]byte(ev.Data), &d) != nil {
				return nil
			}
			state := getOrCreateToolCallState(byOutputIndex, byItemID, &orderedToolCalls, d.OutputIndex, d.ItemID)
			state.Arguments += d.Delta
		case "response.function_call_arguments.done":
			var d codexResponseFunctionCallArgumentsDoneEvent
			if json.Unmarshal([]byte(ev.Data), &d) != nil {
				return nil
			}
			state := getOrCreateToolCallState(byOutputIndex, byItemID, &orderedToolCalls, d.OutputIndex, d.ItemID)
			if name := strings.TrimSpace(d.Name); name != "" {
				state.Name = name
			}
			if d.Arguments != "" {
				state.Arguments = d.Arguments
			}
		case "response.completed":
			_ = json.Unmarshal([]byte(ev.Data), &completed)
		}
		return nil
	})
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", "failed to parse upstream response")
		return
	}

	model := firstNonEmpty(completed.Response.Model, created.Response.Model, requestedModel)
	id := firstNonEmpty(completed.Response.ID, created.Response.ID, fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()))
	createdAt := firstNonZero(completed.Response.CreatedAt, created.Response.CreatedAt, time.Now().Unix())

	chatToolCalls := buildChatToolCalls(orderedToolCalls)
	messageContent := any(textBuilder.String())
	if textBuilder.Len() == 0 && len(chatToolCalls) > 0 {
		messageContent = nil
	}

	message := map[string]any{"role": "assistant", "content": messageContent}
	finishReason := "stop"
	if len(chatToolCalls) > 0 {
		message["tool_calls"] = chatToolCalls
		finishReason = "tool_calls"
	}

	response := map[string]any{
		"id": id, "object": "chat.completion", "created": createdAt, "model": model,
		"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": finishReason}},
	}

	if completed.Response.Usage.TotalTokens > 0 {
		response["usage"] = map[string]any{
			"prompt_tokens":     completed.Response.Usage.InputTokens,
			"completion_tokens": completed.Response.Usage.OutputTokens,
			"total_tokens":      completed.Response.Usage.TotalTokens,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) streamCodexAsChatCompletions(w http.ResponseWriter, body io.Reader, requestedModel string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeOpenAIError(w, http.StatusInternalServerError, "stream_not_supported", "streaming not supported")
		return
	}

	id := ""
	createdAt := int64(0)
	model := requestedModel
	roleSent := false
	toolCallsSeen := false

	byOutputIndex := map[int]*codexToolCallState{}
	byItemID := map[string]*codexToolCallState{}
	var orderedToolCalls []*codexToolCallState

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	emitChunk := func(delta map[string]any, finishReason any) {
		if id == "" {
			id = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
		}
		if createdAt == 0 {
			createdAt = time.Now().Unix()
		}
		if delta != nil && !roleSent {
			if _, ok := delta["role"]; !ok {
				delta["role"] = "assistant"
			}
			roleSent = true
		}
		chunk := map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": createdAt, "model": model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finishReason}},
		}
		b, _ := json.Marshal(chunk)
		_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
		flusher.Flush()
	}

	emitToolArgs := func(state *codexToolCallState, argDelta string) {
		if argDelta == "" {
			return
		}
		emitChunk(map[string]any{
			"tool_calls": []map[string]any{{"index": state.ToolIndex, "function": map[string]any{"arguments": argDelta}}},
		}, nil)
		state.ArgumentsEmitted = true
	}

	emitToolMeta := func(state *codexToolCallState) {
		if state.MetadataSent {
			return
		}
		delta := map[string]any{"index": state.ToolIndex, "type": "function", "function": map[string]any{}}
		if callID := strings.TrimSpace(state.CallID); callID != "" {
			delta["id"] = callID
		}
		if name := strings.TrimSpace(state.Name); name != "" {
			delta["function"] = map[string]any{"name": name}
		}
		emitChunk(map[string]any{"tool_calls": []map[string]any{delta}}, nil)
		state.MetadataSent = true
		if state.BufferedArgs.Len() > 0 {
			emitToolArgs(state, state.BufferedArgs.String())
			state.BufferedArgs.Reset()
		}
	}

	err := parseSSE(body, func(ev sseEvent) error {
		switch ev.Event {
		case "response.created":
			var c codexResponseCreatedEvent
			if json.Unmarshal([]byte(ev.Data), &c) == nil {
				if c.Response.ID != "" {
					id = c.Response.ID
				}
				if c.Response.CreatedAt > 0 {
					createdAt = c.Response.CreatedAt
				}
				if c.Response.Model != "" {
					model = c.Response.Model
				}
			}
		case "response.output_text.delta":
			var d codexResponseOutputTextDeltaEvent
			if json.Unmarshal([]byte(ev.Data), &d) == nil && d.Delta != "" {
				emitChunk(map[string]any{"content": d.Delta}, nil)
			}
		case "response.output_item.added", "response.output_item.done":
			var itemEv codexResponseOutputItemEvent
			if json.Unmarshal([]byte(ev.Data), &itemEv) != nil || itemEv.Item.Type != "function_call" {
				return nil
			}
			toolCallsSeen = true
			state := getOrCreateToolCallState(byOutputIndex, byItemID, &orderedToolCalls, itemEv.OutputIndex, itemEv.Item.ID)
			applyOutputItemToState(state, itemEv.Item)
			emitToolMeta(state)
			if strings.TrimSpace(itemEv.Item.Arguments) != "" && !state.ArgumentsEmitted && state.BufferedArgs.Len() == 0 {
				emitToolArgs(state, itemEv.Item.Arguments)
				state.Arguments = itemEv.Item.Arguments
			}
		case "response.function_call_arguments.delta":
			var d codexResponseFunctionCallArgumentsDeltaEvent
			if json.Unmarshal([]byte(ev.Data), &d) != nil || d.Delta == "" {
				return nil
			}
			toolCallsSeen = true
			state := getOrCreateToolCallState(byOutputIndex, byItemID, &orderedToolCalls, d.OutputIndex, d.ItemID)
			state.Arguments += d.Delta
			if state.MetadataSent {
				emitToolArgs(state, d.Delta)
			} else {
				state.BufferedArgs.WriteString(d.Delta)
			}
		case "response.function_call_arguments.done":
			var d codexResponseFunctionCallArgumentsDoneEvent
			if json.Unmarshal([]byte(ev.Data), &d) != nil {
				return nil
			}
			toolCallsSeen = true
			state := getOrCreateToolCallState(byOutputIndex, byItemID, &orderedToolCalls, d.OutputIndex, d.ItemID)
			if name := strings.TrimSpace(d.Name); name != "" {
				state.Name = name
			}
			if d.Arguments != "" {
				state.Arguments = d.Arguments
			}
			emitToolMeta(state)
			if !state.ArgumentsEmitted && d.Arguments != "" {
				emitToolArgs(state, d.Arguments)
			}
		case "response.completed":
			var c codexResponseCompletedEvent
			if json.Unmarshal([]byte(ev.Data), &c) == nil {
				if c.Response.ID != "" {
					id = c.Response.ID
				}
				if c.Response.CreatedAt > 0 {
					createdAt = c.Response.CreatedAt
				}
				if c.Response.Model != "" {
					model = c.Response.Model
				}
			}
			fr := "stop"
			if toolCallsSeen {
				fr = "tool_calls"
			}
			emitChunk(map[string]any{}, fr)
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
		}
		return nil
	})

	if err != nil {
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}
}

// --- helpers ---

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}
