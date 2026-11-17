package events

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	app "marketmind/services/market-data-service/internal/application/ingestion"
	domain "marketmind/services/market-data-service/internal/domain/market"
)

func record() app.Record {
	source := domain.Provenance{Provider: "sec-edgar", ExternalID: "1", URL: "https://source.example/metadata.json",
		RetrievedAt: time.Now().UTC().Add(-time.Hour), PayloadSHA256: strings.Repeat("a", 64),
		License: domain.LicensePolicy{Classification: "synthetic-test-only", PermissionReference: "synthetic", RetentionAllowed: true}}
	return app.Record{Kind: "filings", Filings: &domain.Snapshot{Company: domain.Company{CIK: "0000000001", Name: "SYNTHETIC", MetadataSource: source}, Filings: []domain.Filing{}}}
}

func TestReplayIdentityExcludesRetrievalButBindsContent(t *testing.T) {
	first := record()
	key := Identity(first)
	first.Filings.Company.MetadataSource.RetrievedAt = time.Now().UTC()
	if Identity(first) != key {
		t.Fatal("same source re-fetch created new identity")
	}
	first.Filings.Company.Name = "changed"
	if Identity(first) == key {
		t.Fatal("changed content reused replay identity")
	}
}

func TestFailuresAreIdempotentWithinButNotAcrossStages(t *testing.T) {
	failure := app.Failure{Topic: app.FilingsTopic, MessageKey: "record", Value: []byte("{}"), Code: "delivery_exhausted"}
	key := FailureID(failure)
	failure.Attempts = 3
	if FailureID(failure) != key {
		t.Fatal("repeated stage failure changed identity")
	}
	failure.Code = "processing_failed"
	if FailureID(failure) == key {
		t.Fatal("later processing failure erased transport history")
	}
}

func TestClosedEnvelopeAndPayloadValidation(t *testing.T) {
	event, _, err := New(record(), "contract-unit")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(event)
	if _, _, _, err := (Codec{}).Decode(app.FilingsTopic, body); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"unknown schema":   func(v map[string]any) { v["dataschema"] = "urn:unknown" },
		"owner mismatch":   func(v map[string]any) { v["source"] = "urn:marketmind:service:ai-orchestrator" },
		"tenant injection": func(v map[string]any) { v["tenantid"] = "spoofed" },
		"unknown field":    func(v map[string]any) { v["unexpected"] = true },
		"invalid UUID":     func(v map[string]any) { v["id"] = strings.Repeat("a", 36) },
		"noncanonical UTC": func(v map[string]any) { v["time"] = strings.Replace(v["time"].(string), "Z", "+00:00", 1) },
		"changed content": func(v map[string]any) {
			v["data"].(map[string]any)["snapshot"].(map[string]any)["company"].(map[string]any)["name"] = "tampered"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var v map[string]any
			_ = json.Unmarshal(body, &v)
			mutate(v)
			changed, _ := json.Marshal(v)
			if _, _, _, err := (Codec{}).Decode(app.FilingsTopic, changed); err == nil {
				t.Fatal("invalid event admitted")
			}
		})
	}
	if _, _, _, err := (Codec{}).Decode(app.PricesTopic, body); err == nil {
		t.Fatal("topic mismatch")
	}
	if _, _, _, err := (Codec{}).Decode(app.FilingsTopic, append(body, []byte("{}")...)); err == nil {
		t.Fatal("multiple JSON values accepted")
	}
}

func TestInvalidUnlicensedAndFutureSources(t *testing.T) {
	for name, mutate := range map[string]func(*app.Record){
		"unlicensed":      func(v *app.Record) { v.Filings.Company.MetadataSource.License.RetentionAllowed = false },
		"credential URL":  func(v *app.Record) { v.Filings.Company.MetadataSource.URL += "?api_key=secret" },
		"future":          func(v *app.Record) { v.Filings.Company.MetadataSource.RetrievedAt = time.Now().UTC().Add(time.Hour) },
		"mismatched kind": func(v *app.Record) { v.Kind = "prices" },
	} {
		t.Run(name, func(t *testing.T) {
			v := record()
			mutate(&v)
			if _, _, err := New(v, "unit"); err == nil {
				t.Fatal("invalid source admitted")
			}
		})
	}
}

func TestPublishedRawExamplesMatchTheActualCodec(t *testing.T) {
	for _, topic := range []string{app.FilingsTopic, app.EconomicsTopic, app.PricesTopic} {
		body, err := os.ReadFile("../../../../../contracts/events/examples/" + topic + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := (Codec{}).Decode(topic, body); err != nil {
			t.Fatal("example differs from producer/consumer", topic, err)
		}
	}
}
