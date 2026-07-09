package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Zayden16/whoop-mcp-server/internal/whoop"
)

const defaultAuthPort = 8719

func main() {
	clientID := os.Getenv("WHOOP_CLIENT_ID")
	clientSecret := os.Getenv("WHOOP_CLIENT_SECRET")

	if len(os.Args) > 1 && os.Args[1] == "auth" {
		fs := flag.NewFlagSet("auth", flag.ExitOnError)
		port := fs.Int("port", defaultAuthPort, "localhost port for the OAuth callback")
		_ = fs.Parse(os.Args[2:])
		if clientID == "" || clientSecret == "" {
			log.Fatal("WHOOP_CLIENT_ID and WHOOP_CLIENT_SECRET must be set (create an app at https://developer.whoop.com)")
		}
		if err := whoop.Authorize(context.Background(), clientID, clientSecret, *port); err != nil {
			log.Fatalf("authorization failed: %v", err)
		}
		return
	}

	if clientID == "" || clientSecret == "" {
		log.Fatal("WHOOP_CLIENT_ID and WHOOP_CLIENT_SECRET must be set")
	}
	client, err := whoop.NewClient(clientID, clientSecret)
	if err != nil {
		log.Fatalf("creating whoop client: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "whoop", Version: "1.0.0"}, nil)
	registerTools(server, client)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

// --- tool inputs ---

type dateRangeInput struct {
	StartDate string `json:"start_date,omitempty" jsonschema:"start date in YYYY-MM-DD format (inclusive)"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"end date in YYYY-MM-DD format (inclusive)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of records to return (default 25, max 100)"`
}

type cycleIDInput struct {
	CycleID int64 `json:"cycle_id" jsonschema:"the Whoop cycle ID"`
}

type daysInput struct {
	Days int `json:"days,omitempty" jsonschema:"number of days to average over (default 7)"`
}

type emptyInput struct{}

func registerTools(server *mcp.Server, client *whoop.Client) {
	text := func(v any) (*mcp.CallToolResult, any, error) {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	}

	collection := func(path string) func(ctx context.Context, req *mcp.CallToolRequest, in dateRangeInput) (*mcp.CallToolResult, any, error) {
		return func(ctx context.Context, req *mcp.CallToolRequest, in dateRangeInput) (*mcp.CallToolResult, any, error) {
			start, end, err := parseRange(in.StartDate, in.EndDate)
			if err != nil {
				return nil, nil, err
			}
			limit := in.Limit
			if limit <= 0 {
				limit = 25
			}
			if limit > 100 {
				limit = 100
			}
			records, err := client.GetPaginated(ctx, path, start, end, limit)
			if err != nil {
				return nil, nil, err
			}
			return text(records)
		}
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_profile",
		Description: "Get the authenticated user's Whoop profile (name, email, user ID).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.Get(ctx, "/v2/user/profile/basic", nil)
		if err != nil {
			return nil, nil, err
		}
		return text(raw)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_body_measurement",
		Description: "Get the user's body measurements (height, weight, max heart rate).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.Get(ctx, "/v2/user/measurement/body", nil)
		if err != nil {
			return nil, nil, err
		}
		return text(raw)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_cycles",
		Description: "Get Whoop physiological cycles (day strain, kilojoules, heart rate) for a date range. Dates in YYYY-MM-DD.",
	}, collection("/v2/cycle"))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_latest_cycle",
		Description: "Get the most recent Whoop cycle (today's strain and heart rate data).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		records, err := client.GetPaginated(ctx, "/v2/cycle", "", "", 1)
		if err != nil {
			return nil, nil, err
		}
		if len(records) == 0 {
			return nil, nil, fmt.Errorf("no cycles found")
		}
		return text(records[0])
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_recoveries",
		Description: "Get Whoop recovery scores (recovery %, HRV, resting heart rate, SpO2) for a date range. Dates in YYYY-MM-DD. Omit dates for the most recent recoveries.",
	}, collection("/v2/recovery"))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_recovery_for_cycle",
		Description: "Get the recovery score for a specific Whoop cycle ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in cycleIDInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.Get(ctx, fmt.Sprintf("/v2/cycle/%d/recovery", in.CycleID), nil)
		if err != nil {
			return nil, nil, err
		}
		return text(raw)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_sleep",
		Description: "Get Whoop sleep records (duration, stages, efficiency, sleep performance) for a date range. Dates in YYYY-MM-DD.",
	}, collection("/v2/activity/sleep"))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_workouts",
		Description: "Get Whoop workouts (sport, strain, heart rate zones, distance) for a date range. Dates in YYYY-MM-DD.",
	}, collection("/v2/activity/workout"))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_average_strain",
		Description: "Calculate the average day strain over the last N days (default 7).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in daysInput) (*mcp.CallToolResult, any, error) {
		days := in.Days
		if days <= 0 {
			days = 7
		}
		end := time.Now().UTC()
		start := end.AddDate(0, 0, -days)
		records, err := client.GetPaginated(ctx, "/v2/cycle",
			start.Format(time.RFC3339), end.Format(time.RFC3339), 100)
		if err != nil {
			return nil, nil, err
		}
		var sum float64
		var n int
		for _, rec := range records {
			var cycle struct {
				Score *struct {
					Strain float64 `json:"strain"`
				} `json:"score"`
			}
			if err := json.Unmarshal(rec, &cycle); err != nil || cycle.Score == nil {
				continue
			}
			sum += cycle.Score.Strain
			n++
		}
		if n == 0 {
			return nil, nil, fmt.Errorf("no scored cycles found in the last %d days", days)
		}
		return text(map[string]any{
			"days":           days,
			"cycles_counted": n,
			"average_strain": sum / float64(n),
		})
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_auth_status",
		Description: "Check whether the server is authenticated with the Whoop API.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.Get(ctx, "/v2/user/profile/basic", nil)
		if err != nil {
			return text(map[string]any{"authenticated": false, "error": err.Error()})
		}
		var profile map[string]any
		_ = json.Unmarshal(raw, &profile)
		return text(map[string]any{"authenticated": true, "profile": profile})
	})
}

// parseRange converts YYYY-MM-DD dates to the RFC3339 timestamps the Whoop
// API expects. end_date is inclusive: it maps to the start of the next day.
func parseRange(startDate, endDate string) (string, string, error) {
	var start, end string
	if startDate != "" {
		t, err := time.Parse("2006-01-02", startDate)
		if err != nil {
			return "", "", fmt.Errorf("invalid start_date %q: expected YYYY-MM-DD", startDate)
		}
		start = t.UTC().Format(time.RFC3339)
	}
	if endDate != "" {
		t, err := time.Parse("2006-01-02", endDate)
		if err != nil {
			return "", "", fmt.Errorf("invalid end_date %q: expected YYYY-MM-DD", endDate)
		}
		end = t.AddDate(0, 0, 1).UTC().Format(time.RFC3339)
	}
	return start, end, nil
}
