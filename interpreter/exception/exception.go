package exception

import (
	"fmt"
	"strings"

	"github.com/ysugimoto/falco/ast"
	"github.com/ysugimoto/falco/token"
)

type Type string

const (
	RuntimeType Type = "RuntimeException"
	SystemType  Type = "SystemException"
)

type Exception struct {
	Type    Type
	Token   *token.Token
	Message string
}

func (e *Exception) Error() string {
	var file string
	var out string

	if e.Token == nil {
		out = fmt.Sprintf("[%s] %s", e.Type, e.Message)
	} else {
		t := *e.Token
		if t.File != "" {
			file = " in " + t.File
		}

		out = fmt.Sprintf("[%s] %s%s at line: %d, position: %d", e.Type, e.Message, file, t.Line, t.Position)
	}

	// SystemException means problem of falco implementation
	// Output additional message that report URL :-)
	if e.Type == SystemType {
		out += "\n\nThis exception is caused by falco interpreter."
		out += "\nIt maybe a bug, please report to http://github.com/ysugimoto/falco"
	}

	return out
}

func Runtime(t *token.Token, format string, args ...any) *Exception {
	return &Exception{
		Type:    RuntimeType,
		Token:   t,
		Message: fmt.Sprintf(format, args...),
	}
}

func System(format string, args ...any) *Exception {
	return &Exception{
		Type:    SystemType,
		Message: fmt.Sprintf(format, args...),
	}
}

func MaxCallStackExceeded(t *token.Token, stacks []*ast.SubroutineDeclaration) *Exception {
	message := make([]string, len(stacks))
	for i := range stacks {
		message[i] = fmt.Sprintf(
			"%s in %s:%d",
			stacks[i].Name.Value,
			stacks[i].GetMeta().Token.File,
			stacks[i].GetMeta().Token.Line,
		)
	}

	return &Exception{
		Type:  RuntimeType,
		Token: t,
		Message: fmt.Sprintf(
			"max call stack exceeded. Call stack:\n%s",
			strings.Join(message, "\n"),
		),
	}
}
