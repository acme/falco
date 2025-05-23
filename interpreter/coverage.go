package interpreter

import (
	"fmt"
	"strings"

	"github.com/ysugimoto/falco/ast"
	"github.com/ysugimoto/falco/tester/shared"
	"github.com/ysugimoto/falco/token"
)

var fake = &ast.Meta{Token: token.Null}

// Add coverage marker to entire VCL
// Note that our coverage measurement will ignores root declarations like backend, table, etc.
func (i *Interpreter) instrument(vcl *ast.VCL) {
	for _, v := range vcl.Statements {
		if sub, ok := v.(*ast.SubroutineDeclaration); ok {
			i.instrumentSubroutine(sub)
		}
	}
}

// Add coverage marker to subroutine declaration
func (i *Interpreter) instrumentSubroutine(sub *ast.SubroutineDeclaration) {
	var statements []ast.Statement

	statements = append(statements, i.createMarker(shared.CoverageTypeSubroutine, sub))
	statements = append(statements, i.instrumentStatements(sub.Block.Statements)...)
	sub.Block.Statements = statements
}

// Add coverage marker to statements
func (i *Interpreter) instrumentStatements(stmts []ast.Statement) []ast.Statement {
	var statements []ast.Statement

	for j := range stmts {
		statements = append(statements, i.instrumentStatement(stmts[j])...)
		statements = append(statements, stmts[j])
	}

	return statements
}

// Add coverage marker to single statement
func (i *Interpreter) instrumentStatement(stmt ast.Statement) []ast.Statement {
	var statements []ast.Statement

	switch t := stmt.(type) {
	// Statement which has sub block statements
	case *ast.BlockStatement:
		// Only put instrumentation to the block inside statements
		t.Statements = i.instrumentStatements(t.Statements)

	case *ast.IfStatement:
		statements = append(statements, i.createMarker(shared.CoverageTypeStatement, t))
		i.instrumentIfStatement(t)

	case *ast.SwitchStatement:
		statements = append(statements, i.createMarker(shared.CoverageTypeStatement, t))
		i.instrumentSwitchStatement(t)

	// Instrumenting for statement with specific argument expression(s)
	case *ast.FunctionCallStatement:
		statements = append(statements, i.instrumentFunctionCallStatement(t)...)
	case *ast.ErrorStatement:
		statements = append(statements, i.instrumentErrorStatement(t)...)
	case *ast.ReturnStatement:
		statements = append(statements, i.instrumentReturnStatement(t)...)

	// Instrumenting for statement with single expression
	case *ast.SetStatement:
		statements = append(statements, i.createMarker(shared.CoverageTypeStatement, stmt))
		statements = append(statements, i.instrumentExpression(t.Value)...)
	case *ast.AddStatement:
		statements = append(statements, i.createMarker(shared.CoverageTypeStatement, stmt))
		statements = append(statements, i.instrumentExpression(t.Value)...)
	case *ast.LogStatement:
		statements = append(statements, i.createMarker(shared.CoverageTypeStatement, stmt))
		statements = append(statements, i.instrumentExpression(t.Value)...)
	case *ast.SyntheticStatement:
		statements = append(statements, i.createMarker(shared.CoverageTypeStatement, stmt))
		statements = append(statements, i.instrumentExpression(t.Value)...)
	case *ast.SyntheticBase64Statement:
		statements = append(statements, i.createMarker(shared.CoverageTypeStatement, stmt))
		statements = append(statements, i.instrumentExpression(t.Value)...)

	// Default without expression instrument
	default:
		// *ast.EsiStatement
		// *ast.RestartStatement
		// *ast.DeclareStatement
		// *ast.UnsetStatement
		// *ast.RemoveStatement
		// *ast.BreakStatement
		// *ast.FallthroughStatement
		// *ast.GotoStatement
		// *ast.IncludeStatement
		statements = append(statements, i.createMarker(shared.CoverageTypeStatement, stmt))
	}

	return statements
}

// Put conditions and branches instruments to if statement.
// Note that on instrumenting, we need to cover the elseif condition,
// so that we will transform the else-if statement to the nested if-else like:
//
// Before:
//
//	if (condition01) {
//	  consequence01...
//	} else if (condition02) {
//	  consequence02...
//	} else {
//	  alternative...
//	}
//
// After:
//
//	[statement of if statement]
//	if (condition01) {
//	  [branch of condition01_1] - if
//	  [statements of consequence01]
//	  consequence01...
//	} else {
//	  # [branch of condition01_2] - else if
//	  if (condition02) {
//	    [branch of condition02]
//	    [statements of consequence02]
//	    consequence02...
//	  } else {
//	    [branch of condition01_3] - else
//	    [statements of alternative]
//	    alternative...
//	  }
//	}
func (i *Interpreter) instrumentIfStatement(stmt *ast.IfStatement) {
	branch := 1

	// instrument consequence
	stmt.Consequence.Statements = append(
		[]ast.Statement{
			i.createMarker(shared.CoverageTypeBranch, stmt, fmt.Sprint(branch)),
		},
		i.instrumentStatements(stmt.Consequence.Statements)...,
	)

	// Store the else block for if statement
	alternative := stmt.Alternative

	// Transform else-if statement to nested else block
	nest := stmt
	for _, a := range stmt.Another {
		branch++
		a.Keyword = "if"
		i.instrumentIfStatement(a)
		nest.Alternative = &ast.ElseStatement{
			Meta: fake,
			Consequence: &ast.BlockStatement{
				Meta: fake,
				Statements: []ast.Statement{
					i.createMarker(shared.CoverageTypeBranch, stmt, fmt.Sprint(branch)),
					a,
				},
			},
		}
		nest = a
	}
	// Reset root else-if statements
	stmt.Another = nil

	// Attach root else estatement to the nested else statement
	if alternative != nil {
		branch++
		nest.Alternative = alternative
		nest.Alternative.Consequence.Statements = append(
			[]ast.Statement{
				i.createMarker(shared.CoverageTypeBranch, stmt, fmt.Sprint(branch)),
			},
			i.instrumentStatements(nest.Alternative.Consequence.Statements)...,
		)
	}
}

// Put conditions and branches instruments to switch statement.
// Note that on instrumenting, we need to cover for each cases,
//
// Before:
//
//	switch (test) {
//	 case "1":
//	   case01_statements...
//	 case "2":
//	   case02_statements...
//	 default:
//	   default_statements...
//	}
//
// After:
//
//	[statement of switch statement]
//	switch (test) {
//	 case "1":
//	   [branch of switch_1]
//	   [statements of case01_statements]
//	   case01_statements...
//	 case "2":
//	   [branch of switch_2]
//	   [statements of case02_statements]
//	   case02_statements...
//	 default:
//	   [statements of default_statements]
//	   [branch of switch_3]
//	   default_statements...
//	}
func (i *Interpreter) instrumentSwitchStatement(stmt *ast.SwitchStatement) {
	branch := 1

	for _, c := range stmt.Cases {
		c.Statements = append(
			[]ast.Statement{
				i.createMarker(shared.CoverageTypeBranch, stmt, fmt.Sprint(branch)),
				i.createMarker(shared.CoverageTypeBranch, c),
			},
			i.instrumentStatements(c.Statements)...,
		)
		branch++
	}
}

func (i *Interpreter) instrumentFunctionCallStatement(stmt *ast.FunctionCallStatement) []ast.Statement {
	var statements []ast.Statement

	for _, arg := range stmt.Arguments {
		statements = append(statements, i.instrumentExpression(arg)...)
	}

	return statements
}

func (i *Interpreter) instrumentErrorStatement(stmt *ast.ErrorStatement) []ast.Statement {
	var statements []ast.Statement

	statements = append(statements, i.createMarker(shared.CoverageTypeStatement, stmt))
	if stmt.Code != nil {
		statements = append(statements, i.instrumentExpression(stmt.Code)...)
	}
	if stmt.Argument != nil {
		statements = append(statements, i.instrumentExpression(stmt.Argument)...)
	}

	return statements
}

func (i *Interpreter) instrumentReturnStatement(stmt *ast.ReturnStatement) []ast.Statement {
	var statements []ast.Statement

	statements = append(statements, i.createMarker(shared.CoverageTypeStatement, stmt))
	if stmt.ReturnExpression != nil {
		statements = append(statements, i.instrumentExpression(stmt.ReturnExpression)...)
	}

	return statements
}

func (i *Interpreter) instrumentExpression(expr ast.Expression) []ast.Statement {
	var statements []ast.Statement

	switch t := expr.(type) {
	case *ast.FunctionCallExpression:
		for _, arg := range t.Arguments {
			statements = append(statements, i.instrumentExpression(arg)...)
		}
	case *ast.GroupedExpression:
		statements = append(statements, i.instrumentExpression(t.Right)...)
	case *ast.InfixExpression:
		statements = append(statements, i.instrumentExpression(t.Left)...)
		statements = append(statements, i.instrumentExpression(t.Right)...)
	case *ast.PostfixExpression:
		statements = append(statements, i.instrumentExpression(t.Left)...)
	case *ast.PrefixExpression:
		statements = append(statements, i.instrumentExpression(t.Right)...)
	case *ast.IfExpression:
		statements = append(statements, i.instrumentIfExpression(t)...)
	}

	return statements
}

// Put conditions and branches instruments to if expression.
// Note that on instrumenting, we need to cover the consequence/alternative expression.
//
// Before:
//
//	set req.http.Foo = if(req.http.Bar, "a", "b");
//
// After:
//
//	[statement of set statement]
//	[statement of if expression]
//	if (req.http.Bar) {
//	  [branch of "a"]
//	} else {
//	  [branch of "b"]
//	}
//	set req.http.Foo = if(req.http.Bar, "a", "b");
func (i *Interpreter) instrumentIfExpression(expr *ast.IfExpression) []ast.Statement {
	branch := &ast.IfStatement{
		Keyword:   "if",
		Meta:      fake,
		Condition: expr.Condition,
		Consequence: &ast.BlockStatement{
			Meta: fake,
			Statements: []ast.Statement{
				i.createMarker(shared.CoverageTypeBranch, expr, "true"),
			},
		},
		Alternative: &ast.ElseStatement{
			Meta: fake,
			Consequence: &ast.BlockStatement{
				Meta: fake,
				Statements: []ast.Statement{
					i.createMarker(shared.CoverageTypeBranch, expr, "false"),
				},
			},
		},
	}

	return []ast.Statement{branch}
}

// Create coverage marker and put cover function into the VCL statements
func (i *Interpreter) createMarker(t shared.CoverageType, node ast.Node, suffix ...string) ast.Statement {
	name := "coverage." + t.String()
	tok := node.GetMeta().Token

	var s string
	if len(suffix) > 0 {
		s = "_" + strings.Join(suffix, "_")
	}

	var id string
	switch t {
	case shared.CoverageTypeSubroutine:
		id = fmt.Sprintf("sub_%d_%d", tok.Line, tok.Position) + s
		i.ctx.Coverage.SetupSubroutine(id, node)
	case shared.CoverageTypeStatement:
		id = fmt.Sprintf("stmt_%d_%d", tok.Line, tok.Position) + s
		i.ctx.Coverage.SetupStatement(id, node)
	case shared.CoverageTypeBranch:
		id = fmt.Sprintf("branch_%d_%d", tok.Line, tok.Position) + s
		i.ctx.Coverage.SetupBranch(id, node)
	}

	return &ast.FunctionCallStatement{
		Meta: fake,
		Function: &ast.Ident{
			Meta: &ast.Meta{
				Token: token.Token{Type: token.STRING, Literal: name},
			},
			Value: name,
		},
		Arguments: []ast.Expression{
			&ast.String{
				Meta: &ast.Meta{
					Token: token.Token{Type: token.STRING, Literal: id},
				},
				Value: id,
			},
		},
	}
}
