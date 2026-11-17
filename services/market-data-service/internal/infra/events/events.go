package events

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	app "marketmind/services/market-data-service/internal/application/ingestion"
)

type Codec struct{}

var _ app.Decoder = Codec{}

// The identity excludes retrieval time, but includes each native content hash,
// query identity and reviewed rights. Identical re-fetches share a replay key.
func Identity(record app.Record) string {
	identities := map[string]bool{}
	for _, source := range record.Sources() {
		identities[source.Provider+"\x00"+source.ExternalID+"\x00"+source.URL+"\x00"+source.PayloadSHA256+
			"\x00"+source.License.Classification+"\x00"+source.License.PermissionReference] = true
	}
	parts := []string{record.Kind}
	for identity := range identities {
		parts = append(parts, identity)
	}
	sort.Strings(parts[1:])
	// Also bind normalized content, so changing a value without changing native
	// provenance cannot silently reuse an already processed replay key.
	body, _ := json.Marshal(record.Snapshot())
	var canonical any
	_ = json.Unmarshal(body, &canonical)
	withoutRetrievalTimes(canonical)
	body, _ = json.Marshal(canonical)
	parts = append(parts, string(body))
	return Hash(strings.Join(parts, "\x01"))
}

func withoutRetrievalTimes(value any) {
	switch value := value.(type) {
	case map[string]any:
		delete(value, "retrieved_at")
		for _, child := range value {
			withoutRetrievalTimes(child)
		}
	case []any:
		for _, child := range value {
			withoutRetrievalTimes(child)
		}
	}
}

func Hash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func New(record app.Record, correlationID string) (app.Envelope, app.RawData, error) {
	if err := record.Validate(); err != nil {
		return app.Envelope{}, app.RawData{}, err
	}
	if !validSourceURLs(record) {
		return app.Envelope{}, app.RawData{}, app.ErrInvalidData
	}
	if !app.ValidToken(correlationID) {
		return app.Envelope{}, app.RawData{}, fmt.Errorf("invalid correlation ID")
	}
	snapshot, err := json.Marshal(record.Snapshot())
	if err != nil {
		return app.Envelope{}, app.RawData{}, err
	}
	data := app.RawData{IdempotencyKey: Identity(record), Kind: record.Kind, Snapshot: snapshot}
	payload, _ := json.Marshal(data)
	event := envelope(record.Topic(), correlationID, payload)
	for _, source := range record.Sources() {
		if source.RetrievedAt.After(event.Time) {
			return app.Envelope{}, app.RawData{}, app.ErrInvalidData
		}
	}
	body, _ := json.Marshal(event)
	if len(body) > app.MaxMessageBytes {
		return app.Envelope{}, app.RawData{}, fmt.Errorf("snapshot exceeds bounded Kafka message size; split the requested range")
	}
	return event, data, nil
}

func envelope(topic, correlationID string, data json.RawMessage) app.Envelope {
	return app.Envelope{SpecVersion: "1.0", ID: NewID(),
		Source: "urn:marketmind:service:market-data-service", Type: topic, Time: time.Now().UTC(),
		DataContentType: "application/json", DataSchema: app.Schema(topic), CorrelationID: correlationID, Data: data}
}

func NewID() string {
	var id [16]byte
	_, _ = rand.Read(id[:]) // crypto/rand.Read is infallible on the pinned Go runtime.
	id[6], id[8] = id[6]&0x0f|0x40, id[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}

func strict(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return app.ErrInvalidData
	}
	if decoder.Decode(new(any)) != io.EOF {
		return app.ErrInvalidData
	}
	return nil
}

func (Codec) Decode(topic string, body []byte) (app.Envelope, app.RawData, app.Record, error) {
	var event app.Envelope
	var data app.RawData
	var record app.Record
	invalid := func() (app.Envelope, app.RawData, app.Record, error) {
		return app.Envelope{}, app.RawData{}, app.Record{}, app.ErrInvalidData
	}
	if len(body) > app.MaxMessageBytes || strict(body, &event) != nil ||
		event.SpecVersion != "1.0" || event.Source != "urn:marketmind:service:market-data-service" ||
		event.Type != topic || event.DataSchema != app.Schema(topic) || event.DataContentType != "application/json" ||
		!app.ValidToken(event.CorrelationID) || event.Time.IsZero() || event.Time.After(time.Now().UTC().Add(time.Minute)) ||
		len(event.ID) != 36 || strings.Trim(strings.ReplaceAll(event.ID, "-", ""), "0123456789abcdef") != "" ||
		event.ID[8] != '-' || event.ID[13] != '-' || event.ID[18] != '-' || event.ID[23] != '-' ||
		strict(event.Data, &data) != nil || !app.ValidHash(data.IdempotencyKey) {
		return invalid()
	}
	record.Kind = data.Kind
	switch data.Kind {
	case "filings":
		record.Filings = newFilings()
	case "economics":
		record.Economics = newEconomics()
	case "prices":
		record.Prices = newPrices()
	default:
		return invalid()
	}
	if record.Topic() != topic || strict(data.Snapshot, record.Snapshot()) != nil || record.Validate() != nil || !validSourceURLs(record) || Identity(record) != data.IdempotencyKey {
		return invalid()
	}
	var wire any
	if json.Unmarshal(body, &wire) != nil || !utcFields(wire) {
		return invalid()
	}
	for _, source := range record.Sources() {
		if source.RetrievedAt.After(event.Time) {
			return invalid()
		}
	}
	return event, data, record, nil
}

func validSourceURLs(record app.Record) bool {
	for _, source := range record.Sources() {
		parsed, err := url.Parse(source.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
			return false
		}
		for key := range parsed.Query() {
			if lower := strings.ToLower(key); lower == "api_key" || lower == "apikey" || lower == "access_token" {
				return false
			}
		}
	}
	return true
}

func utcFields(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "time" || key == "retrieved_at" || key == "published_at" {
				if child == nil && key == "published_at" {
					continue
				}
				text, ok := child.(string)
				if !ok || !strings.HasSuffix(text, "Z") {
					return false
				}
			}
			if !utcFields(child) {
				return false
			}
		}
	case []any:
		for _, child := range value {
			if !utcFields(child) {
				return false
			}
		}
	}
	return true
}

func FailureID(failure app.Failure) string {
	// Deduplicate repeated failures at one stage without erasing a subsequent
	// processing failure after a transport-failure replay of the same message.
	return Hash(failure.Topic + "\x00" + failure.MessageKey + "\x00" + failure.Code + "\x00" + string(failure.Value))
}

func DeadLetter(failure app.Failure) (app.Envelope, app.RawData) {
	id := FailureID(failure)
	payload, _ := json.Marshal(map[string]any{
		"failure_id": id, "original_topic": failure.Topic, "original_key": failure.MessageKey,
		"original_base64": base64.StdEncoding.EncodeToString(failure.Value), "code": failure.Code,
		"attempts": failure.Attempts, "failed_at": failure.FailedAt.UTC(),
	})
	return envelope(app.DLQTopic, "dlq:"+id[:32], payload), app.RawData{IdempotencyKey: id}
}
