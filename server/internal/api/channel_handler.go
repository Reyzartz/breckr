package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"breckr-server/internal/notifier"
	"breckr-server/internal/store"
	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

type ChannelHandler struct {
	logger     *log.Logger
	channels   store.ChannelStore
	dispatcher notifier.Dispatcher
}

func NewChannelHandler(
	logger *log.Logger,
	channels store.ChannelStore,
	dispatcher notifier.Dispatcher,
) *ChannelHandler {
	return &ChannelHandler{logger: logger, channels: channels, dispatcher: dispatcher}
}

func (ch *ChannelHandler) fail(w http.ResponseWriter, err error, what string) {
	if utils.WriteValidationError(w, err) {
		return
	}
	ch.logger.Printf("ERROR: %s: %v", what, err)
	utils.WriteError(w, http.StatusInternalServerError, "internal server error", "")
}

// present converts a stored channel into the API shape.
//
// This is the one place secrets could escape, so it is the one place that has to
// be right: Config is always the spec's redacted view, never the decrypted blob.
// A broken channel presents with an empty config rather than a partial one --
// there is nothing trustworthy to show.
func present(channel *store.StoredChannel) types.Channel {
	view := channel.Channel
	view.Config = map[string]any{}

	if !channel.Broken {
		if spec, err := notifier.ParseSpec(channel.Type, channel.Config); err == nil {
			view.Config = spec.Redacted()
		} else {
			// Decrypted fine but no longer parses -- same practical problem as a
			// failed decrypt, so the dashboard should treat it the same way.
			view.Broken = true
		}
	}

	return view
}

func (ch *ChannelHandler) HandleGetAllChannels(w http.ResponseWriter, r *http.Request) {
	stored, err := ch.channels.ListChannels()
	if err != nil {
		ch.fail(w, err, "ListChannels")
		return
	}

	channels := make([]types.Channel, 0, len(stored))
	for _, channel := range stored {
		channels = append(channels, present(channel))
	}

	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{"data": channels})
}

func (ch *ChannelHandler) HandleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var body types.CreateChannelRequest
	if err := utils.ReadRequestBody(r, &body); err != nil {
		ch.logger.Printf("ERROR: decoding create channel request body: %v", err)
		utils.WriteError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	name, err := validateChannelName(body.Name)
	if err != nil {
		ch.fail(w, err, "validating create channel request")
		return
	}

	if !types.IsChannelType(string(body.Type)) {
		ch.fail(w, utils.Fail("type", "Unknown channel type %q.", body.Type), "validating channel type")
		return
	}

	// Parsed and validated before anything is written, so a rejected config
	// never reaches the database and the error names the offending field.
	if _, err := notifier.ParseAndValidate(body.Type, body.Config); err != nil {
		ch.fail(w, err, "validating channel config")
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	created, err := ch.channels.CreateChannel(store.CreateChannelInput{
		ID:      utils.NewID(),
		Name:    name,
		Type:    body.Type,
		Config:  body.Config,
		Enabled: enabled,
	})
	if err != nil {
		if isUniqueViolation(err) {
			utils.WriteError(w, http.StatusConflict,
				"A channel named \""+name+"\" already exists.", "name")
			return
		}
		ch.fail(w, err, "CreateChannel")
		return
	}

	utils.WriteJSONResponse(w, http.StatusCreated, utils.Envelope{"data": present(created)})
}

func (ch *ChannelHandler) HandleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	id := utils.ReadIDParam(r)

	existing, err := ch.channels.GetChannel(id)
	if err != nil {
		ch.fail(w, err, "GetChannel")
		return
	}
	if existing == nil {
		utils.WriteError(w, http.StatusNotFound, "Unknown channel \""+id+"\".", "")
		return
	}

	var body types.UpdateChannelRequest
	if err := utils.ReadRequestBody(r, &body); err != nil {
		ch.logger.Printf("ERROR: decoding update channel request body: %v", err)
		utils.WriteError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	patch := store.UpdateChannelInput{Enabled: body.Enabled}

	if body.Name != nil {
		name, err := validateChannelName(*body.Name)
		if err != nil {
			ch.fail(w, err, "validating update channel request")
			return
		}
		patch.Name = &name
	}

	if body.Config != nil {
		merged, err := mergeConfig(existing, body.Config)
		if err != nil {
			ch.fail(w, err, "validating channel config")
			return
		}
		patch.Config = merged
	}

	if patch.IsEmpty() {
		utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{"data": present(existing)})
		return
	}

	updated, err := ch.channels.UpdateChannel(id, patch)
	if err != nil {
		if isUniqueViolation(err) {
			utils.WriteError(w, http.StatusConflict, "A channel with that name already exists.", "name")
			return
		}
		ch.fail(w, err, "UpdateChannel")
		return
	}

	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{"data": present(updated)})
}

// mergeConfig lets the dashboard submit a config without re-sending secrets.
//
// The form is populated from the redacted view, so an unchanged secret comes
// back as "••••1234" or empty. Either would overwrite a working credential with
// a mask, so a blank or masked field falls back to what is stored -- and a
// channel whose stored config cannot be read demands the full config instead of
// silently merging into garbage.
func mergeConfig(existing *store.StoredChannel, incoming json.RawMessage) (json.RawMessage, error) {
	var submitted map[string]any
	if err := json.Unmarshal(incoming, &submitted); err != nil {
		return nil, utils.Fail("config", "Configuration is not valid JSON: %v", err)
	}

	stored := map[string]any{}
	if !existing.Broken {
		if err := json.Unmarshal(existing.Config, &stored); err != nil {
			return nil, utils.Fail("config", "The stored configuration could not be read. Re-enter every field.")
		}
	}

	for key, value := range submitted {
		text, isString := value.(string)
		if isString && (text == "" || strings.HasPrefix(text, "••••")) {
			// Left untouched in the form. Keep whatever is stored; if nothing is,
			// the omission surfaces as a normal validation error below.
			continue
		}
		stored[key] = value
	}

	merged, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}

	if _, err := notifier.ParseAndValidate(existing.Type, merged); err != nil {
		return nil, err
	}

	return merged, nil
}

// HandleDeleteChannel removes a channel. Its task links go with it; its run
// history survives under the name it was sent with.
func (ch *ChannelHandler) HandleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := utils.ReadIDParam(r)

	deleted, err := ch.channels.DeleteChannel(id)
	if err != nil {
		ch.fail(w, err, "DeleteChannel")
		return
	}
	if !deleted {
		utils.WriteError(w, http.StatusNotFound, "Unknown channel \""+id+"\".", "")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleTestChannel sends one real notification through a saved channel.
//
// It exists because the alternative way to find out whether alerts work is to
// author a task, wait for its condition to actually fire, and see whether
// anything arrives -- by which point a misconfiguration has already cost the
// alert it was meant to deliver.
func (ch *ChannelHandler) HandleTestChannel(w http.ResponseWriter, r *http.Request) {
	id := utils.ReadIDParam(r)

	channel, err := ch.channels.GetChannel(id)
	if err != nil {
		ch.fail(w, err, "GetChannel")
		return
	}
	if channel == nil {
		utils.WriteError(w, http.StatusNotFound, "Unknown channel \""+id+"\".", "")
		return
	}

	ch.respondWithTest(w, ch.dispatchTest(channel))
}

// HandleTestDraftChannel tests a config that has not been saved, so a wrong
// token is caught while the form is still open rather than after saving it.
func (ch *ChannelHandler) HandleTestDraftChannel(w http.ResponseWriter, r *http.Request) {
	var body types.TestChannelRequest
	if err := utils.ReadRequestBody(r, &body); err != nil {
		ch.logger.Printf("ERROR: decoding test channel request body: %v", err)
		utils.WriteError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	if !types.IsChannelType(string(body.Type)) {
		ch.fail(w, utils.Fail("type", "Unknown channel type %q.", body.Type), "validating channel type")
		return
	}

	if _, err := notifier.ParseAndValidate(body.Type, body.Config); err != nil {
		ch.fail(w, err, "validating channel config")
		return
	}

	ch.respondWithTest(w, ch.dispatchTest(&store.StoredChannel{
		Channel: types.Channel{Name: "Draft channel", Type: body.Type},
		Config:  body.Config,
	}))
}

// dispatchTest sends the test message through the same dispatcher a real alert
// takes -- a token that parses but the API rejects is exactly the failure a
// config check would miss.
func (ch *ChannelHandler) dispatchTest(channel *store.StoredChannel) types.NotificationOutcome {
	// Not r.Context(): a client that navigates away mid-request would otherwise
	// cancel the send, and report a delivery failure that never happened.
	ctx, cancel := context.WithTimeout(context.Background(), types.NotifyTimeout)
	defer cancel()

	outcome := ch.dispatcher.DispatchChannel(ctx, channel, notifier.Message{
		Subject: types.TestNotificationSubject,
		Body:    types.TestNotificationMessage,
	})

	ch.logger.Printf("INFO: test notification attempted (channel=%q type=%s status=%s delivered=%t)",
		channel.Name, channel.Type, outcome.Reason, outcome.Delivered)

	return outcome
}

func (ch *ChannelHandler) respondWithTest(w http.ResponseWriter, outcome types.NotificationOutcome) {
	// Always 200: a rejection by the transport is a successful report of a
	// failed delivery, not an HTTP error -- same as TestTaskResponse.
	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{
		"data": types.TestNotificationResponse{
			OK:          outcome.Delivered,
			Status:      outcome.Reason,
			Detail:      outcome.Detail,
			Message:     types.TestNotificationMessage,
			AttemptedAt: utils.Timestamp(),
		},
	})
}

func validateChannelName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", utils.Fail("name", "Name is required.")
	}
	if len(name) > types.MaxChannelNameLength {
		return "", utils.Fail("name", "Name must be %d characters or fewer.", types.MaxChannelNameLength)
	}
	return name, nil
}

// isUniqueViolation spots the UNIQUE constraint on channels.name, so a duplicate
// answers 409 against the name field rather than a generic 500.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE CONSTRAINT FAILED")
}
