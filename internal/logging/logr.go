package logging

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/rs/zerolog"
)

type sink struct {
	logger zerolog.Logger
	names  []string
	values []any
}

func ControllerRuntime(component string) logr.Logger {
	return logr.New(&sink{logger: Component(component)})
}

func (s *sink) Init(logr.RuntimeInfo) {}

func (s *sink) Enabled(int) bool { return true }

func (s *sink) Info(_ int, msg string, keysAndValues ...any) {
	event := s.logger.Info()
	s.apply(event, keysAndValues...)
	event.Msg(msg)
}

func (s *sink) Error(err error, msg string, keysAndValues ...any) {
	event := s.logger.Error().Err(err)
	s.apply(event, keysAndValues...)
	event.Msg(msg)
}

func (s *sink) WithValues(keysAndValues ...any) logr.LogSink {
	clone := *s
	clone.values = append(append([]any{}, s.values...), keysAndValues...)
	return &clone
}

func (s *sink) WithName(name string) logr.LogSink {
	clone := *s
	clone.names = append(append([]string{}, s.names...), name)
	return &clone
}

func (s *sink) apply(event *zerolog.Event, keysAndValues ...any) {
	if len(s.names) > 0 {
		event.Str("logger", strings.Join(s.names, "/"))
	}
	all := append(append([]any{}, s.values...), keysAndValues...)
	if len(all)%2 != 0 {
		all = append(all, "<missing>")
	}
	for i := 0; i < len(all); i += 2 {
		key, ok := all[i].(string)
		if !ok {
			key = fmt.Sprintf("non_string_key_%d", i/2)
		}
		event.Interface(key, all[i+1])
	}
}
