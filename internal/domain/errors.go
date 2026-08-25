package domain

import "fmt"

type Error struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func NewError(code, message string) *Error { return &Error{Code: code, Message: message} }

func FieldError(code, message, field, detail string) *Error {
	return &Error{Code: code, Message: message, Fields: map[string]string{field: detail}}
}

func WrapInvalid(field string, value any) *Error {
	return FieldError("VALIDATION_FAILED", "请求内容校验失败", field, fmt.Sprintf("无效值：%v", value))
}
