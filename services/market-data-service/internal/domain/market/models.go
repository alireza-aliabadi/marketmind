package market

import (
	"fmt"
	"strings"
	"time"
)

type CIK string

// Central Index Key
// Identifies whose SEC EDGAR submissions

func ParseCIK(raw string) (CIK, error) {
	value := strings.TrimSpace(raw)
	if len(value) == 0 || len(value) > 10 {
		return "", fmt.Errorf(
			"!!!CIK must contain 1-10 decimal digits!!!",
		)
	}

	for _, character := range value {
		if character < '0' || character > '9' {
			return "", fmt.Errorf(
				"!!!CIK must contain only decimal digits!!!",
			)
		}
	}

	if strings.Trim(value, "0") == "" {
		return "", fmt.Errorf(
			"!!!CIK must be non-zero!!!",
		)
	}

	return CIK(strings.Repeat("0", 10-len(value)) + value), nil
}

// PayloadSHA256 hashes the retrieved metadata JSON, not a filing document.
type Provenance struct {
	Provider      string        `json:"provider"`
	ExternalID    string        `json:"external_id"`
	URL           string        `json:"url"`
	RetrievedAt   time.Time     `json:"retrieved_at"`
	PayloadSHA256 string        `json:"payload_sha256"`
	License       LicensePolicy `json:"license"`
}

type Company struct {
	CIK            CIK        `json:"cik"`
	Name           string     `json:"name"`
	Tickers        []string   `json:"tickers"`
	Exchanges      []string   `json:"exchange"`
	EIN            string     `json:"ein"`
	SIC            string     `json:"sic"`
	MetadataSource Provenance `json:"metadata_source"`
}

type Filing struct {
	CIK                   CIK        `json:"cik"`
	AccessionNumber       string     `json:"accession_number"`
	Form                  string     `json:"form"`
	FilingDate            string     `json:"filing_date"`
	ReportDate            string     `json:"report_date"`
	AcceptanceDateTime    string     `json:"acceptance_date_time"`
	PrimaryDoc            string     `json:"primary_doc"`
	PrimaryDocDescription string     `json:"primary_doc_description"`
	DocumentURL           string     `json:"document_url"`
	IndexURL              string     `json:"index_url"`
	PublishedAt           *time.Time `json:"published_at"`
	MetadataSource        Provenance `json:"metadata_source"`
}

type Snapshot struct {
	Company         Company  `json:"company"`
	Filings         []Filing `json:"filings"`
	HistoryComplete bool     `json:"history_complete"`
}
