package postgres

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
	app "marketmind/services/market-data-service/internal/application/ingestion"
	domain "marketmind/services/market-data-service/internal/domain/market"
	"marketmind/services/market-data-service/internal/infra/events"
)

func sourceID(source domain.Provenance) string {
	return events.Hash(source.Provider + "\x00" + source.ExternalID + "\x00" + source.URL + "\x00" + source.PayloadSHA256 +
		"\x00" + source.License.Classification + "\x00" + source.License.PermissionReference)
}

func saveSource(ctx context.Context, tx pgx.Tx, source domain.Provenance) (string, error) {
	id := sourceID(source)
	_, err := tx.Exec(ctx, `INSERT INTO market_data.data_sources
        (source_id,provider,external_id,source_url,payload_sha256,first_retrieved_at,last_retrieved_at,
         license_classification,permission_reference,retention_allowed,ai_allowed)
        VALUES ($1,$2,$3,$4,$5,$6,$6,$7,$8,$9,$10)
        ON CONFLICT (source_id) DO UPDATE SET
        first_retrieved_at=least(market_data.data_sources.first_retrieved_at,EXCLUDED.first_retrieved_at),
        last_retrieved_at=greatest(market_data.data_sources.last_retrieved_at,EXCLUDED.last_retrieved_at)`,
		id, source.Provider, source.ExternalID, source.URL, source.PayloadSHA256, source.RetrievedAt,
		source.License.Classification, source.License.PermissionReference, source.License.RetentionAllowed, source.License.AIAllowed)
	return id, err
}

func saveRecord(ctx context.Context, tx pgx.Tx, record app.Record) error {
	if record.Filings != nil {
		company := record.Filings.Company
		companyID := "cik:" + string(company.CIK)
		payload, _ := json.Marshal(company)
		if _, err := tx.Exec(ctx, `INSERT INTO market_data.companies (company_id,cik,name,source_id,observed_at,payload)
            VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (company_id) DO UPDATE
            SET name=EXCLUDED.name,source_id=EXCLUDED.source_id,observed_at=EXCLUDED.observed_at,payload=EXCLUDED.payload
            WHERE EXCLUDED.observed_at > market_data.companies.observed_at`,
			companyID, company.CIK, company.Name, sourceID(company.MetadataSource), company.MetadataSource.RetrievedAt, payload); err != nil {
			return err
		}
		for _, filing := range record.Filings.Filings {
			payload, _ := json.Marshal(filing)
			if _, err := tx.Exec(ctx, `INSERT INTO market_data.filings
                (accession_number,company_id,form,filing_date,report_date,published_at,document_url,source_id,observed_at,payload)
            VALUES ($1,$2,$3,$4::text::date,nullif($5,'')::date,$6,nullif($7,''),$8,$9,$10)
                ON CONFLICT (accession_number) DO UPDATE SET form=EXCLUDED.form,filing_date=EXCLUDED.filing_date,
                report_date=EXCLUDED.report_date,published_at=EXCLUDED.published_at,document_url=EXCLUDED.document_url,
                source_id=EXCLUDED.source_id,observed_at=EXCLUDED.observed_at,payload=EXCLUDED.payload
                WHERE EXCLUDED.observed_at > market_data.filings.observed_at`,
				filing.AccessionNumber, companyID, filing.Form, filing.FilingDate, filing.ReportDate, filing.PublishedAt,
				filing.DocumentURL, sourceID(filing.MetadataSource), filing.MetadataSource.RetrievedAt, payload); err != nil {
				return err
			}
		}
	}
	if record.Economics != nil {
		series := record.Economics.Series
		payload, _ := json.Marshal(series)
		if _, err := tx.Exec(ctx, `INSERT INTO market_data.economic_series
            (series_id,name,units,frequency,seasonal_adjustment,notices,source_id,observed_at,payload)
            VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (series_id) DO UPDATE SET
            name=EXCLUDED.name,units=EXCLUDED.units,frequency=EXCLUDED.frequency,
            seasonal_adjustment=EXCLUDED.seasonal_adjustment,notices=EXCLUDED.notices,
            source_id=EXCLUDED.source_id,observed_at=EXCLUDED.observed_at,payload=EXCLUDED.payload
            WHERE EXCLUDED.observed_at > market_data.economic_series.observed_at`,
			series.ID, series.Name, series.Units, series.Frequency, series.SeasonalAdjustment, series.Notes,
			sourceID(series.MetadataSource), series.MetadataSource.RetrievedAt, payload); err != nil {
			return err
		}
		for _, observation := range record.Economics.Observations {
			quality, value := "missing", (*string)(nil)
			if observation.Value != nil {
				quality = "valid"
				decimal := string(*observation.Value)
				value = &decimal
			}
			payload, _ := json.Marshal(observation)
			if _, err := tx.Exec(ctx, `INSERT INTO market_data.economic_observations
                (series_id,period_date,realtime_start,realtime_end,value,quality,source_id,observed_at,payload)
                VALUES ($1,$2::text::date,$3::text::date,$4::text::date,$5::text::numeric,$6,$7,$8,$9)
                ON CONFLICT (series_id,period_date,realtime_start,realtime_end) DO UPDATE
                SET value=EXCLUDED.value,quality=EXCLUDED.quality,source_id=EXCLUDED.source_id,
                observed_at=EXCLUDED.observed_at,payload=EXCLUDED.payload
                WHERE EXCLUDED.observed_at > market_data.economic_observations.observed_at`,
				series.ID, observation.Date, observation.RealtimeStart, observation.RealtimeEnd, value, quality,
				sourceID(observation.MetadataSource), observation.MetadataSource.RetrievedAt, payload); err != nil {
				return err
			}
		}
	}
	if record.Prices != nil {
		instrument := record.Prices.Instrument
		payload, _ := json.Marshal(instrument)
		if _, err := tx.Exec(ctx, `INSERT INTO market_data.instruments (instrument_id,company_id,name,source_id,observed_at,payload)
            VALUES ($1,nullif($2,''),$3,$4,$5,$6) ON CONFLICT (instrument_id) DO UPDATE
            SET company_id=EXCLUDED.company_id,name=EXCLUDED.name,source_id=EXCLUDED.source_id,
            observed_at=EXCLUDED.observed_at,payload=EXCLUDED.payload
            WHERE EXCLUDED.observed_at > market_data.instruments.observed_at`,
			instrument.ID, instrument.CompanyID, instrument.Name, sourceID(instrument.MetadataSource), instrument.MetadataSource.RetrievedAt, payload); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO market_data.instrument_listings
            (instrument_id,ticker,exchange_mic,currency,as_of_date,list_date,source_id,observed_at)
            VALUES ($1,$2,$3,$4,$5::text::date,nullif($6,'')::date,$7,$8)
            ON CONFLICT (instrument_id,exchange_mic,as_of_date) DO UPDATE SET ticker=EXCLUDED.ticker,
            currency=EXCLUDED.currency,list_date=EXCLUDED.list_date,source_id=EXCLUDED.source_id,observed_at=EXCLUDED.observed_at
            WHERE EXCLUDED.observed_at > market_data.instrument_listings.observed_at`,
			instrument.ID, instrument.Ticker, instrument.Exchange, instrument.Currency, instrument.AsOfDate, instrument.ListDate,
			sourceID(instrument.MetadataSource), instrument.MetadataSource.RetrievedAt); err != nil {
			return err
		}
		for _, bar := range record.Prices.Bars {
			payload, _ := json.Marshal(bar)
			if _, err := tx.Exec(ctx, `INSERT INTO market_data.market_prices
                (time,instrument_id,provider,exchange_mic,currency,adjustment,delay_mode,delay_seconds,
                 open,high,low,close,volume,source_id,observed_at,payload)
                VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::text::numeric,$10::text::numeric,$11::text::numeric,$12::text::numeric,$13::text::numeric,$14,$15,$16)
                ON CONFLICT (time,instrument_id,provider,exchange_mic,currency,adjustment) DO UPDATE
                SET open=EXCLUDED.open,high=EXCLUDED.high,low=EXCLUDED.low,close=EXCLUDED.close,volume=EXCLUDED.volume,
                delay_mode=EXCLUDED.delay_mode,delay_seconds=EXCLUDED.delay_seconds,source_id=EXCLUDED.source_id,
                observed_at=EXCLUDED.observed_at,payload=EXCLUDED.payload
                WHERE EXCLUDED.observed_at > market_data.market_prices.observed_at`,
				bar.Time, instrument.ID, bar.MetadataSource.Provider, instrument.Exchange, instrument.Currency,
				bar.Adjustment, bar.Delay.Mode, bar.Delay.Seconds, string(bar.Open), string(bar.High), string(bar.Low),
				string(bar.Close), string(bar.Volume), sourceID(bar.MetadataSource), bar.MetadataSource.RetrievedAt, payload); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repository) Inspect(ctx context.Context, kind, id string) ([]json.RawMessage, error) {
	queries := map[string]string{
		"company": `SELECT payload FROM market_data.companies WHERE company_id=$1`,
		"filing":  `SELECT payload FROM market_data.filings WHERE accession_number=$1`,
		"economic": `SELECT o.payload || jsonb_build_object('series_id',s.series_id,'units',s.units)
            FROM market_data.economic_observations o JOIN market_data.economic_series s USING (series_id)
            WHERE series_id=$1 ORDER BY period_date,realtime_start LIMIT 100`,
		"prices": `SELECT payload || jsonb_build_object('instrument_id',instrument_id,'currency',currency,
            'exchange',exchange_mic) FROM market_data.market_prices WHERE instrument_id=$1
            ORDER BY time DESC,adjustment LIMIT 100`,
	}
	query, ok := queries[kind]
	if !ok || strings.TrimSpace(id) == "" {
		return nil, app.ErrInvalidData
	}
	rows, err := r.pool.Query(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		result = append(result, payload)
	}
	return result, rows.Err()
}
