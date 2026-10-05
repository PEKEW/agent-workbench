package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
)

// Answer 只回应本会话仍待处理的原生请求，不透传任意协议方法。
func (m *Manager) Answer(ctx context.Context, id, requestID string, result json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	r := m.sessions[id]
	if r == nil || r.client == nil || r.State == "closed" || r.State == "disconnected" {
		m.mu.Unlock()
		return errors.New("会话未连接")
	}
	var request *Request
	for i := range r.Requests {
		if r.Requests[i].ID == requestID {
			copy := r.Requests[i]
			request = &copy
			break
		}
	}
	if request == nil || r.answering[requestID] {
		m.mu.Unlock()
		return errors.New("原生请求已解决或正在回应")
	}
	if err := validateAnswer(*request, result); err != nil {
		m.mu.Unlock()
		return err
	}
	r.answering[requestID] = true
	c := r.client
	payload, _ := json.Marshal(map[string]any{"requestId": requestID, "method": request.Method, "result": loggedAnswer(*request, result)})
	if err := m.recordLocked(r, "workbench/requestAnswerSubmitted", nil, payload); err != nil {
		delete(r.answering, requestID)
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	err := c.replyIf(json.RawMessage(requestID), result, func() error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if r.client != c || r.State == "closed" || r.State == "disconnected" {
			return errors.New("原生连接已失效")
		}
		for _, pending := range r.Requests {
			if pending.ID == requestID {
				return ctx.Err()
			}
		}
		return errors.New("原生请求已被撤销，未发送回应")
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(r.answering, requestID)
	if err != nil {
		return err
	}
	for i, q := range r.Requests {
		if q.ID == requestID {
			r.Requests = append(r.Requests[:i], r.Requests[i+1:]...)
			break
		}
	}
	if len(r.Requests) == 0 && r.State == "waiting" {
		r.State = "running"
	}
	return m.persistLocked(r)
}
func validateAnswer(q Request, result json.RawMessage) error {
	var response map[string]json.RawMessage
	if json.Unmarshal(result, &response) != nil || response == nil {
		return errors.New("原生响应必须为 JSON 对象")
	}
	switch q.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		var decision string
		if len(response) != 1 || json.Unmarshal(response["decision"], &decision) != nil || decision != "accept" && decision != "decline" && decision != "cancel" {
			return errors.New("只支持本次允许、拒绝或取消")
		}
	case "item/permissions/requestApproval":
		var answer struct {
			Permissions any    `json:"permissions"`
			Scope       string `json:"scope"`
		}
		var params struct {
			Permissions any `json:"permissions"`
		}
		_ = json.Unmarshal(q.Params, &params)
		if json.Unmarshal(result, &answer) != nil || len(response) != 2 || answer.Scope != "turn" || answer.Permissions == nil {
			return errors.New("权限授权只能用于当前回合")
		}
		if !reflect.DeepEqual(answer.Permissions, map[string]any{}) && !reflect.DeepEqual(answer.Permissions, params.Permissions) {
			return errors.New("只能授予原生请求的权限或拒绝授权")
		}
	case "item/tool/requestUserInput":
		var answer struct {
			Answers map[string]struct {
				Answers []string `json:"answers"`
			} `json:"answers"`
		}
		var params struct {
			Questions []struct {
				ID string `json:"id"`
			} `json:"questions"`
		}
		_ = json.Unmarshal(q.Params, &params)
		if len(response) != 1 || json.Unmarshal(result, &answer) != nil || len(answer.Answers) != len(params.Questions) {
			return errors.New("回答必须对应原生问题")
		}
		for _, question := range params.Questions {
			a, ok := answer.Answers[question.ID]
			if !ok || len(a.Answers) == 0 {
				return errors.New("请回答全部原生问题")
			}
		}
	case "mcpServer/elicitation/request":
		var action string
		if json.Unmarshal(response["action"], &action) != nil || action != "cancel" {
			return errors.New("当前仅支持取消 MCP elicitation")
		}
	default:
		return errors.New("此原生请求暂不支持，请中断或关闭会话")
	}
	return nil
}

// loggedAnswer 遮蔽原生明确标记 isSecret 的回答，只影响日志，不改实际响应。
func loggedAnswer(request Request, result json.RawMessage) json.RawMessage {
	if request.Method != "item/tool/requestUserInput" {
		return result
	}
	var params struct {
		Questions []struct {
			ID       string `json:"id"`
			IsSecret bool   `json:"isSecret"`
		} `json:"questions"`
	}
	if json.Unmarshal(request.Params, &params) != nil {
		return result
	}
	var answer map[string]any
	if json.Unmarshal(result, &answer) != nil {
		return result
	}
	answers, ok := answer["answers"].(map[string]any)
	if !ok {
		return result
	}
	for _, question := range params.Questions {
		if question.IsSecret {
			answers[question.ID] = map[string]any{"answers": []string{"[已隐藏敏感回答]"}}
		}
	}
	logged, err := json.Marshal(answer)
	if err != nil {
		return result
	}
	return logged
}
