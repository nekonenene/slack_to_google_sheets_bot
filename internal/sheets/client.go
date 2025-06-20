package sheets

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type Client struct {
	service *sheets.Service
}

func NewClient(credentialsJSON string) (*Client, error) {
	ctx := context.Background()
	
	var credentialsData []byte
	var err error
	
	// Check if credentialsJSON is a file path or JSON content
	// File path criteria: shorter than 512 chars, ends with .json, and doesn't start with {
	isFilePath := len(credentialsJSON) < 512 && 
		strings.HasSuffix(credentialsJSON, ".json") && 
		!strings.HasPrefix(strings.TrimSpace(credentialsJSON), "{")
	
	if isFilePath {
		// It's likely a file path, try to read the file
		credentialsData, err = os.ReadFile(credentialsJSON)
		if err != nil {
			return nil, fmt.Errorf("unable to read credentials file '%s': %v", credentialsJSON, err)
		}
		log.Printf("Read credentials from file: %s (%d bytes)", credentialsJSON, len(credentialsData))
	} else {
		// It's JSON content
		credentialsData = []byte(credentialsJSON)
		log.Printf("Using credentials as JSON content (%d bytes)", len(credentialsData))
	}

	service, err := sheets.NewService(ctx, option.WithCredentialsJSON(credentialsData))
	if err != nil {
		return nil, fmt.Errorf("unable to create sheets service: %v", err)
	}

	return &Client{service: service}, nil
}

type MessageRecord struct {
	Timestamp   time.Time
	Channel     string
	ChannelName string
	User        string
	UserName    string
	Text        string
	ThreadTS    string
}

func (c *Client) WriteMessage(spreadsheetID string, record *MessageRecord) error {
	// Prepare the row data
	values := []interface{}{
		record.Timestamp.Format("2006-01-02 15:04:05"),
		record.ChannelName,
		record.UserName,
		record.Text,
		record.ThreadTS,
	}

	// Check if sheet exists, create if not
	if err := c.ensureSheetExists(spreadsheetID, "Messages"); err != nil {
		return err
	}

	// Append the row
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{values},
	}

	_, err := c.service.Spreadsheets.Values.Append(
		spreadsheetID,
		"Messages!A:E",
		valueRange,
	).ValueInputOption("RAW").Do()

	if err != nil {
		return fmt.Errorf("unable to write data to sheet: %v", err)
	}

	return nil
}

func (c *Client) ensureSheetExists(spreadsheetID, sheetName string) error {
	// Get spreadsheet info
	spreadsheet, err := c.service.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("unable to get spreadsheet: %v", err)
	}

	// Check if sheet exists
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == sheetName {
			return nil // Sheet exists
		}
	}

	// Create the sheet
	requests := []*sheets.Request{
		{
			AddSheet: &sheets.AddSheetRequest{
				Properties: &sheets.SheetProperties{
					Title: sheetName,
				},
			},
		},
	}

	batchUpdateRequest := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: requests,
	}

	_, err = c.service.Spreadsheets.BatchUpdate(spreadsheetID, batchUpdateRequest).Do()
	if err != nil {
		return fmt.Errorf("unable to create sheet: %v", err)
	}

	// Add headers
	headers := []interface{}{
		"Timestamp",
		"Channel",
		"User",
		"Message",
		"Thread ID",
	}

	headerRange := &sheets.ValueRange{
		Values: [][]interface{}{headers},
	}

	_, err = c.service.Spreadsheets.Values.Update(
		spreadsheetID,
		sheetName+"!A1:E1",
		headerRange,
	).ValueInputOption("RAW").Do()

	if err != nil {
		log.Printf("Warning: unable to add headers: %v", err)
	}

	return nil
}

func (c *Client) EnsureSheetExists(spreadsheetID, sheetName string) error {
	return c.ensureSheetExists(spreadsheetID, sheetName)
}
