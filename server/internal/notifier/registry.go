package notifier

import (
	"encoding/json"
	"fmt"
	"log"

	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

// entry is one supported destination kind: how to read its config, and how to
// turn that config into something that sends.
type entry struct {
	parse func(raw json.RawMessage) (Spec, error)
	build func(spec Spec, logger *log.Logger) Transport
}

// registry is the single place that knows the set of transports. Adding one is
// a new spec, a new transport and a line here -- nothing else in the codebase
// switches on channel type.
var registry = map[types.ChannelType]entry{
	types.ChannelTelegram: {
		parse: parseInto[TelegramSpec],
		build: func(spec Spec, logger *log.Logger) Transport {
			return NewTelegram(spec.(*TelegramSpec), logger)
		},
	},
	types.ChannelDiscord: {
		parse: parseInto[DiscordSpec],
		build: func(spec Spec, logger *log.Logger) Transport {
			return NewDiscord(spec.(*DiscordSpec), logger)
		},
	},
	types.ChannelSlack: {
		parse: parseInto[SlackSpec],
		build: func(spec Spec, logger *log.Logger) Transport {
			return NewSlack(spec.(*SlackSpec), logger)
		},
	},
	types.ChannelWebhook: {
		parse: parseInto[WebhookSpec],
		build: func(spec Spec, logger *log.Logger) Transport {
			return NewWebhook(spec.(*WebhookSpec), logger)
		},
	},
	types.ChannelEmail: {
		parse: parseInto[EmailSpec],
		build: func(spec Spec, logger *log.Logger) Transport {
			return NewEmail(spec.(*EmailSpec), logger)
		},
	},
}

// parseInto decodes the stored blob into one spec type. Generic so each registry
// entry is a type argument rather than a near-identical closure.
func parseInto[T any](raw json.RawMessage) (Spec, error) {
	spec := new(T)
	if err := json.Unmarshal(raw, spec); err != nil {
		return nil, utils.Fail("config", "Configuration is not valid JSON: %v", err)
	}

	typed, ok := any(spec).(Spec)
	if !ok {
		// Unreachable: every registered type implements Spec on its pointer.
		return nil, fmt.Errorf("%T does not implement Spec", spec)
	}
	return typed, nil
}

// ParseSpec decodes a channel's config without validating it. Callers that are
// accepting user input should follow with Spec.Validate.
func ParseSpec(kind types.ChannelType, raw json.RawMessage) (Spec, error) {
	found, ok := registry[kind]
	if !ok {
		return nil, utils.Fail("type", "Unknown channel type %q.", kind)
	}
	return found.parse(raw)
}

// ParseAndValidate is what the API layer wants: both halves, one error.
func ParseAndValidate(kind types.ChannelType, raw json.RawMessage) (Spec, error) {
	spec, err := ParseSpec(kind, raw)
	if err != nil {
		return nil, err
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return spec, nil
}

// Build turns a parsed spec into a transport that can send.
func Build(kind types.ChannelType, spec Spec, logger *log.Logger) (Transport, error) {
	found, ok := registry[kind]
	if !ok {
		return nil, utils.Fail("type", "Unknown channel type %q.", kind)
	}
	return found.build(spec, logger), nil
}

// BuildFromConfig is the path a stored channel takes on its way to a send.
func BuildFromConfig(kind types.ChannelType, raw json.RawMessage, logger *log.Logger) (Transport, error) {
	spec, err := ParseSpec(kind, raw)
	if err != nil {
		return nil, err
	}
	return Build(kind, spec, logger)
}
