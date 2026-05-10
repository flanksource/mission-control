// Mirror of postProcessLogs from playbook/actions/logs.go. Kept in-tree so
// the plugin doesn't have to import the host's playbook/actions package
// (which would pull most of the host server in via init() side effects).
// If you change the algorithm, change both copies in lockstep.
package main

import (
	"sort"
	"time"

	"github.com/flanksource/commons/duration"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/logs"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

func postProcessLogs(ctx dutyContext.Context, logLines []*logs.LogLine, postProcess v1.LogsPostProcess) []*logs.LogLine {
	if postProcess.Empty() {
		return logLines
	}

	var messageFields []string
	if postProcess.Mapping != nil {
		messageFields = postProcess.Mapping.Message
	}

	if postProcess.Parse != "" {
		for _, line := range logLines {
			logs.ParseMessage(line, postProcess.Parse)
		}
	}

	matched := matchLogs(ctx, logLines, postProcess.Match, messageFields)
	return dedupeLogs(matched, postProcess.Dedupe, messageFields)
}

func dedupeLogs(logLines []*logs.LogLine, dedupe *v1.LogDedupe, messageFields []string) []*logs.LogLine {
	if dedupe == nil {
		return logLines
	}

	sort.Slice(logLines, func(i, j int) bool {
		return logLines[i].FirstObserved.Before(logLines[j].FirstObserved)
	})

	if dedupe.Window != "" {
		window, err := duration.ParseDuration(dedupe.Window)
		if err != nil {
			return logLines
		}

		windowed := divideLogsByWindow(logLines, time.Duration(window))

		out := make([]*logs.LogLine, 0, len(logLines))
		for _, bucket := range windowed {
			out = append(out, dedupeWindow(bucket, dedupe.Fields, messageFields)...)
		}
		return out
	}

	return dedupeWindow(logLines, dedupe.Fields, messageFields)
}

func divideLogsByWindow(logLines []*logs.LogLine, window time.Duration) [][]*logs.LogLine {
	logsByWindow := make([][]*logs.LogLine, 0, len(logLines))

	var currentWindowStart time.Time
	var currentWindow []*logs.LogLine

	for _, line := range logLines {
		logWindow := line.FirstObserved.Truncate(window)
		if currentWindowStart.IsZero() {
			currentWindowStart = logWindow
			currentWindow = append(currentWindow, line)
			continue
		}

		if logWindow.Equal(currentWindowStart) {
			currentWindow = append(currentWindow, line)
			continue
		}

		logsByWindow = append(logsByWindow, currentWindow)
		currentWindow = []*logs.LogLine{line}
		currentWindowStart = logWindow
	}

	if len(currentWindow) > 0 {
		logsByWindow = append(logsByWindow, currentWindow)
	}

	return logsByWindow
}

func dedupeWindow(logLines []*logs.LogLine, fields []string, messageFields []string) []*logs.LogLine {
	out := make([]*logs.LogLine, 0, len(logLines))
	seen := make(map[string]*logs.LogLine)

	for _, line := range logLines {
		key := line.GetFieldKey(fields, messageFields...)

		previous, found := seen[key]
		if !found {
			seen[key] = line
			out = append(out, line)
			continue
		}

		previous.Count += line.Count
		if line.FirstObserved.Before(previous.FirstObserved) {
			previous.FirstObserved = line.FirstObserved
		}

		if line.LastObserved != nil {
			if previous.LastObserved == nil || line.LastObserved.After(*previous.LastObserved) {
				previous.LastObserved = line.LastObserved
			}
		} else if !line.FirstObserved.IsZero() {
			previous.LastObserved = &line.FirstObserved
		}

		previous.Message = line.Message
		previous.Host = line.Host
		previous.Severity = line.Severity
		previous.Source = line.Source
	}

	return out
}

func matchLogs(ctx dutyContext.Context, logLines []*logs.LogLine, matchExprs []types.MatchExpression, messageFields []string) []*logs.LogLine {
	if len(matchExprs) == 0 {
		return logLines
	}

	faulty := make(map[string]struct{})
	var out []*logs.LogLine

outer:
	for _, line := range logLines {
		env := line.TemplateContext(messageFields...)

		for _, expr := range matchExprs {
			if _, ok := faulty[string(expr)]; ok {
				continue
			}

			tmpl := gomplate.Template{Expression: string(expr)}
			ok, err := gomplate.RunTemplateBool(env, tmpl)
			if err != nil {
				ctx.Logger.V(4).Infof("failed to evaluate match expression '%s': %v", expr, err)
				faulty[string(expr)] = struct{}{}
				continue
			}

			if ok {
				out = append(out, line)
				continue outer
			}
		}
	}

	return out
}
