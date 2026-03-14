// Package reports generates business and operations reports.
package reports

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

var reportSeq atomic.Int64

// ReportResult contains the generated report output.
type ReportResult struct {
	ID      string
	Summary string
	Data    []byte
	Format  string
}

// Generate produces a report of the given type for the date range.
func Generate(reportType string, from, to time.Time, filters []string) ReportResult {
	reportSeq.Add(1)
	id := fmt.Sprintf("rpt-%06d", reportSeq.Load())

	payload := buildPayload(reportType, from, to, filters)
	data, _ := json.MarshalIndent(payload, "", "  ")

	return ReportResult{
		ID:      id,
		Summary: buildSummary(reportType, from, to),
		Data:    data,
		Format:  "JSON",
	}
}

func buildSummary(reportType string, from, to time.Time) string {
	period := "N/A"
	if !from.IsZero() && !to.IsZero() {
		period = fmt.Sprintf("%s – %s",
			from.Format("2006-01-02"),
			to.Format("2006-01-02"),
		)
	}
	switch reportType {
	case "FINANCIAL":
		return fmt.Sprintf("Stark Industries financial summary for %s. Revenue up 12%%. R&D spend at 18%% of revenue.", period)
	case "OPERATIONS":
		return fmt.Sprintf("Operations report for %s. 7 active projects. 2 incidents resolved. 99.97%% uptime.", period)
	case "THREAT_SUMMARY":
		return fmt.Sprintf("Threat summary for %s. 3 new threats detected. 1 neutralised. Current threat level: ELEVATED.", period)
	default:
		return fmt.Sprintf("Report type %q generated for period %s.", reportType, period)
	}
}

func buildPayload(reportType string, from, to time.Time, filters []string) map[string]any {
	return map[string]any{
		"report_type": reportType,
		"generated":   time.Now().Format(time.RFC3339),
		"period": map[string]string{
			"from": from.Format(time.RFC3339),
			"to":   to.Format(time.RFC3339),
		},
		"filters": filters,
		"sections": []string{"executive_summary", "key_metrics", "action_items"},
		"metrics": map[string]any{
			"total_records_analysed": 14_872,
			"anomalies_detected":     3,
			"recommendations":        2,
		},
	}
}
