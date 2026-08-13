package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/cel-go/cel"
	celtypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/types/known/structpb"
)

func newProjectionEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("source", cel.DynType),
		cel.Variable("target", cel.DynType),
		cel.Variable("context", cel.DynType),
		cel.Variable("item", cel.DynType),
		cel.Function("text", cel.Overload(
			"projection_text_dyn",
			[]*cel.Type{cel.DynType},
			cel.StringType,
			cel.UnaryBinding(projectionText),
		)),
		cel.Function("date", cel.Overload(
			"projection_date_dyn",
			[]*cel.Type{cel.DynType},
			cel.StringType,
			cel.UnaryBinding(projectionDate),
		)),
		cel.Function("title", cel.Overload(
			"projection_title_dyn",
			[]*cel.Type{cel.DynType},
			cel.StringType,
			cel.UnaryBinding(projectionTitle),
		)),
		cel.Function("number", cel.Overload(
			"projection_number_dyn",
			[]*cel.Type{cel.DynType},
			cel.DynType,
			cel.UnaryBinding(projectionNumber),
		)),
	)
}

// projectionNumber parses a catalog property into a number a register can record as one.
// Config properties arrive as strings — "17" alert counts, a "6.9" score — and base CEL
// has no int(string) or double(string) overload, so a mapping would otherwise have to
// store a measurement as prose. A value that is not numeric is an error rather than a
// zero, because a silent zero reads as "no findings".
//
// A whole value returns as an integer so a count writes to YAML as `17` rather than
// `17.0`; only a genuinely fractional value stays a double.
func projectionNumber(value ref.Val) ref.Val {
	if value == celtypes.NullValue || value.Value() == nil {
		return celtypes.NewErr("number() does not accept null")
	}
	var parsed float64
	switch typed := value.Value().(type) {
	case string:
		converted, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return celtypes.NewErr("number() cannot parse %q", typed)
		}
		parsed = converted
	case int64:
		return celtypes.Int(typed)
	case float64:
		parsed = typed
	default:
		return celtypes.NewErr("number() expects a string or numeric value, got %T", value.Value())
	}
	if math.IsNaN(parsed) {
		return celtypes.NewErr("number() does not accept NaN")
	}
	// The range test is not decoration: converting a float64 outside int64's range is
	// undefined in Go and yields the minimum int64 on amd64, so a byte count past 2^63
	// would land in a register as a large negative number. A value too big to be an int
	// is still a number and stays a double — the int conversion exists only so a count
	// writes as `17` rather than `17.0`. The bounds also subsume the infinities, and the
	// upper one is strictly less than 2^63 because math.MaxInt64 is not representable as
	// a float64 and rounds up to exactly that.
	if parsed == math.Trunc(parsed) && parsed >= math.MinInt64 && parsed < 1<<63 {
		return celtypes.Int(int64(parsed))
	}
	return celtypes.Double(parsed)
}

// projectionTitle capitalises each word so vendor-lowercased identifiers such as a
// Linux distribution name read as proper nouns in a register. Base CEL has no case
// functions, so mappings would otherwise need a ternary per known value.
func projectionTitle(value ref.Val) ref.Val {
	if value == celtypes.NullValue || value.Value() == nil {
		return celtypes.NewErr("title() does not accept null")
	}
	text, ok := value.Value().(string)
	if !ok {
		return celtypes.NewErr("title() expects a string, got %T", value.Value())
	}
	words := strings.Split(text, " ")
	for index, word := range words {
		if word == "" {
			continue
		}
		runes := []rune(word)
		runes[0] = unicode.ToUpper(runes[0])
		words[index] = string(runes)
	}
	return celtypes.String(strings.Join(words, " "))
}

func projectionText(value ref.Val) ref.Val {
	if value == celtypes.NullValue || value.Value() == nil {
		return celtypes.NewErr("text() does not accept null")
	}
	return celtypes.String(fmt.Sprint(value.Value()))
}

// projectionDate renders a timestamp as the YYYY-MM-DD form every register uses.
// Base CEL has no string slicing, so mappings cannot derive it themselves.
func projectionDate(value ref.Val) ref.Val {
	if value == celtypes.NullValue || value.Value() == nil {
		return celtypes.NewErr("date() does not accept null")
	}
	switch typed := value.Value().(type) {
	case time.Time:
		return celtypes.String(typed.UTC().Format(registerDateLayout))
	case string:
		parsed, err := time.Parse(time.RFC3339, typed)
		if err != nil {
			return celtypes.NewErr("date() expects an RFC3339 timestamp, got %q", typed)
		}
		return celtypes.String(parsed.UTC().Format(registerDateLayout))
	default:
		return celtypes.NewErr("date() expects a timestamp or RFC3339 string, got %T", value.Value())
	}
}

func compileProjectionExpression(env *cel.Env, expression string) (cel.Program, error) {
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return env.Program(ast)
}

func evalProjectionBool(program cel.Program, activation map[string]any) (bool, error) {
	value, _, err := program.Eval(activation)
	if err != nil {
		return false, err
	}
	matched, ok := value.Value().(bool)
	if !ok {
		return false, fmt.Errorf("expression returned %T, expected bool", value.Value())
	}
	return matched, nil
}

func evalProjectionValue(program cel.Program, activation map[string]any) (any, error) {
	value, _, err := program.Eval(activation)
	if err != nil {
		return nil, err
	}
	return projectionNativeValue(value)
}

func projectionNativeValue(value ref.Val) (any, error) {
	if value == celtypes.NullValue || value.Value() == nil {
		return nil, nil
	}
	native, err := value.ConvertToNative(celtypes.JSONValueType)
	if err != nil {
		return nil, err
	}
	jsonValue, ok := native.(*structpb.Value)
	if !ok {
		return nil, fmt.Errorf("CEL value converted to %T, expected JSON value", native)
	}
	return projectionWholeNumbers(jsonValue.AsInterface()), nil
}

// projectionWholeNumbers narrows integral floats back to integers. Every CEL result is
// routed through structpb, whose only numeric type is a float64, so a count of two would
// otherwise be written to a register as `2.0` and a schema declaring it an integer would
// be describing something the file does not look like. A fractional value — a Scorecard
// score of 6.9 — is left alone.
func projectionWholeNumbers(value any) any {
	switch typed := value.(type) {
	case float64:
		if typed == math.Trunc(typed) && !math.IsInf(typed, 0) && math.Abs(typed) < 1<<53 {
			return int64(typed)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = projectionWholeNumbers(item)
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			typed[key] = projectionWholeNumbers(item)
		}
		return typed
	default:
		return value
	}
}
