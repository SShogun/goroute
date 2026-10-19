package proxy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type ExtractedToolCall struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

var (
	toolUseRegex  = regexp.MustCompile(`(?s)<tool_use>\s*(\{.*?\})\s*</tool_use>`)
	toolCallRegex = regexp.MustCompile(`(?s)<tool_call>\s*(\{.*?\})\s*</tool_call>`)
	invokeRegex   = regexp.MustCompile(`(?s)<invoke\s+name="([^"]+)"\s*>(.*?)</invoke>`)
	paramRegex    = regexp.MustCompile(`(?s)<parameter\s+name="([^"]+)"[^>]*>(.*?)</parameter>`)
)

// ExtractXmlToolCalls finds <tool_use>, <tool_call>, or <invoke> blocks in static text
func ExtractXmlToolCalls(text string) (string, []ExtractedToolCall) {
	parser := NewXmlStreamParser()
	cleaned, calls := parser.Feed(text)
	leftover := parser.Flush()
	return strings.TrimSpace(cleaned + leftover), calls
}

// XmlStreamParser handles streaming text deltas and intercepts XML tool calls across chunks.
type XmlStreamParser struct {
	buffer string
}

func NewXmlStreamParser() *XmlStreamParser {
	return &XmlStreamParser{}
}

func (p *XmlStreamParser) Feed(chunk string) (string, []ExtractedToolCall) {
	p.buffer += chunk
	var cleanText strings.Builder
	var calls []ExtractedToolCall

	for {
		// Find earliest opening tag
		idxUse := strings.Index(p.buffer, "<tool_use>")
		idxCall := strings.Index(p.buffer, "<tool_call>")
		locInvoke := regexp.MustCompile(`<invoke\s+name="([^"]+)"\s*>`).FindStringSubmatchIndex(p.buffer)

		// Determine earliest match
		earliestType := ""
		earliestIdx := -1
		tagLen := 0
		invokeName := ""

		if idxUse != -1 && (earliestIdx == -1 || idxUse < earliestIdx) {
			earliestIdx = idxUse
			earliestType = "tool_use"
			tagLen = len("<tool_use>")
		}
		if idxCall != -1 && (earliestIdx == -1 || idxCall < earliestIdx) {
			earliestIdx = idxCall
			earliestType = "tool_call"
			tagLen = len("<tool_call>")
		}
		if locInvoke != nil && (earliestIdx == -1 || locInvoke[0] < earliestIdx) {
			earliestIdx = locInvoke[0]
			earliestType = "invoke"
			tagLen = locInvoke[1] - locInvoke[0]
			invokeName = p.buffer[locInvoke[2]:locInvoke[3]]
		}

		if earliestIdx == -1 {
			// No opening tag found. Check if the end of buffer might be a partial opening tag like "<tool"
			lastLt := strings.LastIndex(p.buffer, "<")
			if lastLt != -1 && len(p.buffer)-lastLt < 20 && !strings.Contains(p.buffer[lastLt:], ">") {
				cleanText.WriteString(p.buffer[:lastLt])
				p.buffer = p.buffer[lastLt:]
			} else {
				cleanText.WriteString(p.buffer)
				p.buffer = ""
			}
			break
		}

		// Emit text before the tag
		cleanText.WriteString(p.buffer[:earliestIdx])
		rest := p.buffer[earliestIdx:]

		// Search for closing tag
		closeTag := ""
		switch earliestType {
		case "tool_use":
			closeTag = "</tool_use>"
		case "tool_call":
			closeTag = "</tool_call>"
		case "invoke":
			closeTag = "</invoke>"
		}

		closeIdx := strings.Index(rest, closeTag)
		if closeIdx == -1 {
			// Incomplete tag; keep waiting for more chunks
			p.buffer = rest
			break
		}

		innerContent := rest[tagLen:closeIdx]
		p.buffer = rest[closeIdx+len(closeTag):]

		switch earliestType {
		case "tool_use":
			var parsed struct {
				ID        string                 `json:"id"`
				Name      string                 `json:"name"`
				Input     map[string]interface{} `json:"input"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(innerContent)), &parsed); err == nil && parsed.Name != "" {
				id := parsed.ID
				if id == "" {
					id = fmt.Sprintf("toolu_xml_%d", time.Now().UnixNano())
				}
				input := parsed.Input
				if input == nil && parsed.Arguments != nil {
					input = parsed.Arguments
				}
				if input == nil {
					input = make(map[string]interface{})
				}
				calls = append(calls, ExtractedToolCall{
					ID:    id,
					Name:  parsed.Name,
					Input: input,
				})
			}
		case "tool_call":
			var parsed struct {
				ID        string      `json:"id"`
				Name      string      `json:"name"`
				ToolName  string      `json:"tool_name"`
				Arguments interface{} `json:"arguments"`
				Args      interface{} `json:"args"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(innerContent)), &parsed); err == nil {
				name := parsed.Name
				if name == "" {
					name = parsed.ToolName
				}
				id := parsed.ID
				if id == "" {
					id = fmt.Sprintf("toolu_txt_%d", time.Now().UnixNano())
				}
				input := make(map[string]interface{})
				rawArgs := parsed.Arguments
				if rawArgs == nil {
					rawArgs = parsed.Args
				}
				if rawArgs != nil {
					switch a := rawArgs.(type) {
					case map[string]interface{}:
						input = a
					case string:
						_ = json.Unmarshal([]byte(a), &input)
					}
				}
				if name != "" {
					calls = append(calls, ExtractedToolCall{
						ID:    id,
						Name:  name,
						Input: input,
					})
				}
			}
		case "invoke":
			args := make(map[string]interface{})
			matches := paramRegex.FindAllStringSubmatch(innerContent, -1)
			for _, m := range matches {
				if len(m) >= 3 {
					args[m[1]] = strings.TrimSpace(m[2])
				}
			}
			id := fmt.Sprintf("toolu_inv_%d", time.Now().UnixNano())
			calls = append(calls, ExtractedToolCall{
				ID:    id,
				Name:  invokeName,
				Input: args,
			})
		}
	}

	return cleanText.String(), calls
}

func (p *XmlStreamParser) Flush() string {
	remaining := p.buffer
	p.buffer = ""
	return remaining
}
